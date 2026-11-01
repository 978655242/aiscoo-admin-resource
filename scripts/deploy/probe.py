#!/usr/bin/env python3
"""Independent public acceptance probe. Read-only: it never sends a write request.

Usage: probe.py <origin> <commit-sha>      e.g. probe.py https://112.124.202.60:8443 <40 hex>
The version must equal "release-" + the first 12 hex digits of <commit-sha>: the deployment builds with
-X github.com/QuantumNous/new-api/common.Version=release-<12 hex>, and /api/status reports that string.
Environment (never argv):
  AISCOO_PROBE_TLS=strict|fallback     strict (default): the certificate chain must verify, or the probe fails.
                                       fallback: if (and only if) the chain does NOT verify, continue without
                                       verification, say so loudly ("TLS: UNVERIFIED") and never send a token.
                                       Any other TLS or network error is never downgraded.
  AISCOO_PROBE_ALLOW_HTTP=1            required for an http:// origin (the :8080 test entry)
  AISCOO_PROBE_TOKEN, AISCOO_PROBE_USER_ID   optional; adds an authenticated GET /api/user/self check
  AISCOO_PROBE_TOKEN_OVER_HTTP=1       required to send the token over http://; without it the
                                       authenticated check is skipped, never silently sent in clear text.
                                       The token is also never sent over an unverified TLS connection.
  AISCOO_PROBE_CLOSED_PORTS=3000,5432  ports on the origin host that must NOT accept a TCP connection
  AISCOO_PROBE_NOT_ADMIN_ORIGINS=https://112.124.202.60   comma separated origins whose /api/status must NOT
                                       return JSON (443 belongs to the official website; admin is on 8443 only)
  AISCOO_PROBE_ATTEMPTS, AISCOO_PROBE_DELAY   retry whole check set (default 6 x 5 s)
Stdout ends with "TLS: verified|UNVERIFIED|none" and "ACCEPTANCE: full|basic" (basic = no authenticated check ran).
"""
import html.parser
import json
import os
import re
import socket
import ssl
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

# OpenSSL X509_V_ERR codes for an issuer/root that this runner does not trust: 2 unable to get issuer certificate,
# 18 self-signed certificate, 19 self-signed certificate in chain, 20 unable to get local issuer, 21 unable to verify the first certificate.
UNTRUSTED_CHAIN_CODES = (2, 18, 19, 20, 21)
JS_TYPES = ('text/javascript', 'application/javascript', 'application/x-javascript')


class ProbeError(Exception):
    """Message is fixed text written here; it never embeds server output or credentials."""


class ScriptSources(html.parser.HTMLParser):
    def __init__(self):
        super().__init__()
        self.sources = []

    def handle_starttag(self, tag, attrs):
        if tag.lower() == 'script':
            src = dict(attrs).get('src')
            if src:
                self.sources.append(src)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def parse_origin(origin):
    parsed = urllib.parse.urlsplit(origin)
    if (parsed.scheme not in ('https', 'http') or not parsed.hostname or
            parsed.username or parsed.password or parsed.path not in ('', '/') or
            parsed.query or parsed.fragment):
        raise ValueError('Expected an origin without credentials, path, query or fragment')
    if parsed.scheme == 'http' and os.environ.get('AISCOO_PROBE_ALLOW_HTTP') != '1':
        raise ValueError('HTTP requires AISCOO_PROBE_ALLOW_HTTP=1')
    return parsed


def read_credentials(parsed):
    token = os.environ.get('AISCOO_PROBE_TOKEN', '')
    raw_id = os.environ.get('AISCOO_PROBE_USER_ID', '')
    if not token and not raw_id:
        return None
    if not token or not re.fullmatch(r'[1-9][0-9]{0,18}', raw_id) or re.search(r'[\r\n]', token):
        raise ValueError('AISCOO_PROBE_TOKEN and a positive AISCOO_PROBE_USER_ID must be set together')
    if parsed.scheme == 'http' and os.environ.get('AISCOO_PROBE_TOKEN_OVER_HTTP') != '1':
        print('NOTICE: authenticated check skipped; the token would travel over plain HTTP '
              '(set AISCOO_PROBE_TOKEN_OVER_HTTP=1 to accept that risk)', flush=True)
        return None
    return token, int(raw_id)


class Tls:
    """Decides once whether the chain verifies. The decision is sticky across retries and never silent."""

    def __init__(self, allow_fallback):
        self.allow_fallback = allow_fallback
        self.unverified = False
        self.checked = False

    def context(self, parsed):
        if parsed.scheme != 'https':
            return None
        if self.unverified:
            context = ssl.create_default_context()
            context.check_hostname = False
            context.verify_mode = ssl.CERT_NONE
            return context
        context = ssl.create_default_context()
        if self.checked:
            return context
        port = parsed.port or 443
        try:
            with socket.create_connection((parsed.hostname, port), timeout=10) as raw:
                with context.wrap_socket(raw, server_hostname=parsed.hostname):
                    pass
        except ssl.SSLCertVerificationError as exc:
            # Only "the issuer or root is not trusted here" may be downgraded. An expired certificate, a hostname
            # mismatch or any other TLS/network error is a real failure, never silently passed.
            if exc.verify_code not in UNTRUSTED_CHAIN_CODES:
                raise ProbeError('TLS certificate is invalid for this host (expired, wrong name or revoked); not downgraded')
            if not self.allow_fallback:
                raise ProbeError('TLS certificate chain does not verify on this runner (AISCOO_PROBE_TLS=strict)')
            self.unverified = True
            print('WARNING: the TLS certificate chain could NOT be verified on this runner; continuing WITHOUT '
                  'certificate verification (AISCOO_PROBE_TLS=fallback). No credential will be sent.', flush=True)
            return self.context(parsed)
        self.checked = True
        print('ok: TLS certificate chain verified', flush=True)
        return context


def opener_for(context):
    handlers = [urllib.request.ProxyHandler({}), NoRedirect()]
    if context is not None:
        handlers.append(urllib.request.HTTPSHandler(context=context))
    return urllib.request.build_opener(*handlers)


def check_not_admin(tls, target):
    """The origin (the website on 443) must not answer /api/status with JSON. Any other answer is acceptable."""
    parsed = parse_origin(target)
    opener = opener_for(tls.context(parsed))
    request = urllib.request.Request(target.rstrip('/') + '/api/status', headers={'Accept-Encoding': 'identity'}, method='GET')
    try:
        with opener.open(request, timeout=10) as response:
            code, body = response.status, response.read(1 << 20)
    except urllib.error.HTTPError as error:
        code, body = error.code, error.read(1 << 20)
    try:
        json.loads(body)
    except ValueError:
        print('ok: %s/api/status returned HTTP %d and no JSON (it is not the admin service)' % (target.rstrip('/'), code), flush=True)
        return
    raise ProbeError('%s/api/status returned JSON; the admin service must only be reachable on :8443' % target.rstrip('/'))


def run_checks(parsed, sha, credentials, closed_ports, tls, not_admin):
    origin = parsed.scheme + '://' + parsed.netloc
    expected = 'release-' + sha[:12]
    opener = opener_for(tls.context(parsed))
    if credentials and tls.unverified:
        print('NOTICE: authenticated check skipped; the TLS chain is unverified and no token is sent over it', flush=True)
        credentials = None

    def fetch(path, auth=False):
        url = urllib.parse.urljoin(origin + '/', path)
        target = urllib.parse.urlsplit(url)
        if (target.scheme, target.netloc) != (parsed.scheme, parsed.netloc):
            raise ProbeError('cross-origin resource refused')
        headers = {'Cache-Control': 'no-cache', 'Accept-Encoding': 'identity'}
        if auth:
            headers['Authorization'] = 'Bearer ' + credentials[0]
        request = urllib.request.Request(url, headers=headers, method='GET')
        with opener.open(request, timeout=10) as response:
            if response.status != 200:
                raise ProbeError('unexpected HTTP status')
            body = response.read(10 * 1024 * 1024 + 1)
            if len(body) > 10 * 1024 * 1024:
                raise ProbeError('response exceeds size limit')
            return body, response.headers.get_content_type()

    def status():
        data = json.loads(fetch('/api/status')[0])
        if data.get('success') is not True or not isinstance(data.get('data'), dict):
            raise ProbeError('/api/status did not succeed')
        return data['data']

    state = status()
    if state.get('version') != expected:
        raise ProbeError('public version does not equal the deployed commit (expected release-<first 12 hex>)')
    print('ok: public /api/status reports %s, the deployed commit' % expected, flush=True)
    if state.get('setup') is not True:
        raise ProbeError('system is not initialised (setup is not true)')
    print('ok: setup is true', flush=True)
    if state.get('register_enabled') is not False:
        raise ProbeError('registration is not reported as disabled')
    print('ok: registration is disabled (status flag; no registration request was sent)', flush=True)

    page, content_type = fetch('/')
    if content_type != 'text/html':
        raise ProbeError('homepage is not HTML')
    scripts = ScriptSources()
    scripts.feed(page.decode('utf-8'))
    if not scripts.sources:
        raise ProbeError('homepage references no script')
    for src in scripts.sources:
        body, content_type = fetch(src)
        if len(body) < 100 or content_type not in JS_TYPES:
            raise ProbeError('a homepage script is empty or not JavaScript')
    print('ok: homepage and %d script resource(s) returned 200' % len(scripts.sources), flush=True)

    for port in closed_ports:
        try:
            socket.create_connection((parsed.hostname, port), timeout=5).close()
        except OSError:
            continue
        raise ProbeError('port %d accepts connections from outside' % port)
    if closed_ports:
        print('ok: ports %s are not reachable from the probe' % ','.join(map(str, closed_ports)), flush=True)

    for target in not_admin:
        check_not_admin(tls, target)

    mode = 'basic'
    if credentials:
        account = json.loads(fetch('/api/user/self', auth=True)[0])
        if account.get('success') is not True or account.get('data', {}).get('id') != credentials[1]:
            raise ProbeError('authenticated read-only check failed')
        print('ok: authenticated /api/user/self matches the expected account', flush=True)
        mode = 'full'
    else:
        print('NOTICE: no authenticated business check ran; acceptance is limited to unauthenticated checks', flush=True)

    if status().get('version') != expected:
        raise ProbeError('public version changed during the probe')
    return mode


def main():
    if len(sys.argv) != 3 or not re.fullmatch(r'[0-9a-f]{40}', sys.argv[2]):
        raise ValueError('Usage: probe.py <origin> <commit-sha>')
    parsed = parse_origin(sys.argv[1])
    credentials = read_credentials(parsed)
    ports = os.environ.get('AISCOO_PROBE_CLOSED_PORTS', '')
    if ports and not re.fullmatch(r'[0-9]{1,5}(,[0-9]{1,5})*', ports):
        raise ValueError('AISCOO_PROBE_CLOSED_PORTS must be comma separated ports')
    closed_ports = [int(p) for p in ports.split(',')] if ports else []
    tls_mode = os.environ.get('AISCOO_PROBE_TLS', 'strict')
    if tls_mode not in ('strict', 'fallback'):
        raise ValueError('AISCOO_PROBE_TLS must be strict or fallback')
    tls = Tls(tls_mode == 'fallback')
    not_admin = [o.strip() for o in os.environ.get('AISCOO_PROBE_NOT_ADMIN_ORIGINS', '').split(',') if o.strip()]
    for target in not_admin:
        other = parse_origin(target)
        if other.hostname != parsed.hostname or other.port == parsed.port:
            raise ValueError('AISCOO_PROBE_NOT_ADMIN_ORIGINS must name another port on the same host')
    attempts = int(os.environ.get('AISCOO_PROBE_ATTEMPTS', '6'))
    delay = float(os.environ.get('AISCOO_PROBE_DELAY', '5'))
    for attempt in range(1, attempts + 1):
        try:
            mode = run_checks(parsed, sys.argv[2], credentials, closed_ports, tls, not_admin)
            print('TLS: ' + ('none (plain HTTP)' if parsed.scheme == 'http' else 'UNVERIFIED' if tls.unverified else 'verified'))
            print('ACCEPTANCE: ' + mode)
            return
        except (ProbeError, ValueError, KeyError, TypeError, AttributeError, OSError, UnicodeError,
                urllib.error.URLError, ssl.SSLError) as exc:
            detail = str(exc) if isinstance(exc, ProbeError) else type(exc).__name__
            print('attempt %d/%d failed: %s' % (attempt, attempts, detail), file=sys.stderr, flush=True)
            if attempt == attempts:
                raise ProbeError(detail)
            time.sleep(delay)


if __name__ == '__main__':
    try:
        main()
    except (ProbeError, ValueError) as exc:
        # Only fixed strings or the exception class name are printed; never server output or credentials.
        print('Public acceptance failed: ' + (str(exc) if isinstance(exc, ProbeError) else 'bad usage or configuration'),
              file=sys.stderr)
        sys.exit(1)
