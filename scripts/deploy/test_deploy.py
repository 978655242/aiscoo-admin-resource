#!/usr/bin/env python3
"""Isolated contract tests for probe.py and the deployment files. Never connects to a deployment host."""
import hashlib
import json
import os
import re
import socket
import subprocess
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parent
REPO = ROOT.parent.parent
SHA = 'a' * 40
VERSION = 'release-' + SHA[:12]   # what the build embeds with -X ...common.Version and /api/status reports


class State:
    version = VERSION
    setup = True
    register_enabled = False
    script = b'javascript ' * 20
    account_id = 42
    token_seen = []


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/api/status':
            data = {'version': State.version, 'setup': State.setup, 'register_enabled': State.register_enabled}
            body, kind = json.dumps({'success': True, 'data': data}).encode(), 'application/json'
        elif self.path == '/':
            body, kind = b'<html><script src="/assets/main.js"></script></html>', 'text/html'
        elif self.path == '/assets/main.js':
            body, kind = State.script, 'application/javascript'
        elif self.path == '/api/user/self':
            State.token_seen.append(self.headers.get('Authorization'))
            if self.headers.get('Authorization') != 'Bearer test-token':
                self.send_error(401)
                return
            body, kind = json.dumps({'success': True, 'data': {'id': State.account_id}}).encode(), 'application/json'
        else:
            self.send_error(404)
            return
        self.send_response(200)
        self.send_header('Content-Type', kind)
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


class Probe(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.origin = 'http://127.0.0.1:' + str(cls.server.server_port)

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join()

    def setUp(self):
        State.version, State.setup, State.register_enabled = VERSION, True, False
        State.script, State.token_seen = b'javascript ' * 20, []

    def probe(self, sha=SHA, origin=None, **env):
        base = {k: v for k, v in os.environ.items() if not k.startswith('AISCOO_PROBE_')}
        base.update({'AISCOO_PROBE_ALLOW_HTTP': '1', 'AISCOO_PROBE_ATTEMPTS': '1', 'AISCOO_PROBE_DELAY': '0'})
        base.update(env)
        return subprocess.run([sys.executable, '-I', str(ROOT / 'probe.py'), origin or self.origin, sha],
                              text=True, capture_output=True, env=base)

    def test_unauthenticated_pass_is_reported_as_basic(self):
        result = self.probe()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('ACCEPTANCE: basic', result.stdout)
        self.assertIn('no authenticated business check ran', result.stdout)

    def test_each_failed_condition_fails_the_probe(self):
        cases = {
            'wrong version': lambda: setattr(State, 'version', 'release-' + 'b' * 12),
            'bare 40-hex sha instead of release-<12>': lambda: setattr(State, 'version', SHA),
            'release- with 11 hex digits': lambda: setattr(State, 'version', 'release-' + SHA[:11]),
            'setup false': lambda: setattr(State, 'setup', False),
            'registration enabled': lambda: setattr(State, 'register_enabled', True),
            'empty script': lambda: setattr(State, 'script', b''),
        }
        for name, mutate in cases.items():
            with self.subTest(name):
                mutate()
                try:
                    result = self.probe()
                finally:
                    self.setUp()
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn('ACCEPTANCE', result.stdout)

    def test_expected_sha_must_match_even_if_server_is_consistent(self):
        self.assertNotEqual(self.probe(sha='c' * 40).returncode, 0)

    def test_http_needs_explicit_opt_in(self):
        self.assertNotEqual(self.probe(AISCOO_PROBE_ALLOW_HTTP='0').returncode, 0)

    def test_token_is_never_sent_over_http_without_second_opt_in(self):
        result = self.probe(AISCOO_PROBE_TOKEN='test-token', AISCOO_PROBE_USER_ID='42')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('ACCEPTANCE: basic', result.stdout)
        self.assertEqual(State.token_seen, [])
        self.assertNotIn('test-token', result.stdout + result.stderr)

    def test_authenticated_check_when_opted_in(self):
        env = dict(AISCOO_PROBE_TOKEN='test-token', AISCOO_PROBE_USER_ID='42', AISCOO_PROBE_TOKEN_OVER_HTTP='1')
        result = self.probe(**env)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('ACCEPTANCE: full', result.stdout)
        self.assertNotIn('test-token', result.stdout + result.stderr)
        for bad in (dict(env, AISCOO_PROBE_TOKEN='wrong'), dict(env, AISCOO_PROBE_USER_ID='43')):
            with self.subTest(bad=bad):
                failed = self.probe(**bad)
                self.assertNotEqual(failed.returncode, 0)
                self.assertNotIn('wrong', failed.stdout + failed.stderr)
        self.assertNotEqual(self.probe(AISCOO_PROBE_TOKEN='test-token', AISCOO_PROBE_TOKEN_OVER_HTTP='1').returncode, 0)

    def test_closed_port_check(self):
        self.assertEqual(self.probe(AISCOO_PROBE_CLOSED_PORTS=str(free_port())).returncode, 0)
        result = self.probe(AISCOO_PROBE_CLOSED_PORTS=str(self.server.server_port))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('accepts connections', result.stderr)
        self.assertNotEqual(self.probe(AISCOO_PROBE_CLOSED_PORTS='abc').returncode, 0)

    def test_retries_until_the_new_version_appears(self):
        State.version = 'release-' + 'b' * 12
        timer = threading.Timer(1.0, lambda: setattr(State, 'version', VERSION))
        timer.start()
        try:
            result = self.probe(AISCOO_PROBE_ATTEMPTS='6', AISCOO_PROBE_DELAY='0.5')
        finally:
            timer.cancel()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_version_must_be_release_prefix_of_the_sha(self):
        # Same sha prefix with a different commit tail is the same version string: only the first 12 digits are embedded.
        self.assertEqual(self.probe(sha=SHA[:12] + 'c' * 28).returncode, 0)
        self.assertNotEqual(self.probe(sha='c' * 12 + SHA[12:]).returncode, 0)

    def test_origin_validation(self):
        for origin in ('http://user:pw@127.0.0.1:1', 'http://127.0.0.1:1/path', 'ftp://127.0.0.1', 'http://127.0.0.1:1?x=1'):
            with self.subTest(origin=origin):
                self.assertNotEqual(self.probe(origin=origin).returncode, 0)
        self.assertNotEqual(self.probe(sha='short').returncode, 0)


def openssl(*args, cwd):
    return subprocess.run(['openssl', *args], cwd=cwd, check=True, capture_output=True)


class NotAdminHandler(BaseHTTPRequestHandler):
    """Stands in for the official website on 443: an HTML 404, or (when asked) the admin JSON."""
    as_admin = False

    def do_GET(self):
        if NotAdminHandler.as_admin and self.path == '/api/status':
            body, kind, code = b'{"success":true,"data":{}}', 'application/json', 200
        else:
            body, kind, code = b'<html><body>404 Not Found</body></html>', 'text/html', 404
        self.send_response(code)
        self.send_header('Content-Type', kind)
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


class NotAdmin(Probe):
    """Port 443 (the website) must not answer /api/status with JSON. Runs every Probe test, then the new ones."""
    @classmethod
    def setUpClass(cls):
        super().setUpClass()
        cls.site = ThreadingHTTPServer(('127.0.0.1', 0), NotAdminHandler)
        threading.Thread(target=cls.site.serve_forever, daemon=True).start()
        cls.site_origin = 'http://127.0.0.1:%d' % cls.site.server_port

    @classmethod
    def tearDownClass(cls):
        cls.site.shutdown()
        cls.site.server_close()
        super().tearDownClass()

    def setUp(self):
        super().setUp()
        NotAdminHandler.as_admin = False

    def test_website_404_html_passes(self):
        result = self.probe(AISCOO_PROBE_NOT_ADMIN_ORIGINS=self.site_origin)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('is not the admin service', result.stdout)

    def test_website_returning_json_fails(self):
        NotAdminHandler.as_admin = True
        result = self.probe(AISCOO_PROBE_NOT_ADMIN_ORIGINS=self.site_origin)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('must only be reachable on :8443', result.stderr)
        self.assertNotIn('ACCEPTANCE', result.stdout)

    def test_not_admin_origin_must_be_another_port_of_the_same_host(self):
        self.assertNotEqual(self.probe(AISCOO_PROBE_NOT_ADMIN_ORIGINS=self.origin).returncode, 0)
        self.assertNotEqual(self.probe(AISCOO_PROBE_NOT_ADMIN_ORIGINS='http://localhost:%d' % self.site.server_port).returncode, 0)


class QuietServer(ThreadingHTTPServer):
    def handle_error(self, request, client_address):
        pass   # probes that reject the certificate drop the connection mid-handshake; that is expected


class Tls(unittest.TestCase):
    """HTTPS with a throw-away CA: strict verification, explicit loud fallback, and no downgrade for other TLS errors."""
    @classmethod
    def setUpClass(cls):
        import shutil
        import ssl
        import tempfile
        if not shutil.which('openssl'):
            raise unittest.SkipTest('openssl unavailable')
        cls.dir = tempfile.TemporaryDirectory()
        d = cls.dir.name
        openssl('req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-keyout', 'ca.key', '-out', 'ca.pem', '-days', '2',
                '-subj', '/CN=test-ca', '-addext', 'basicConstraints=critical,CA:TRUE', '-addext', 'keyUsage=critical,keyCertSign',
                '-addext', 'subjectKeyIdentifier=hash', cwd=d)
        for name, san in (('good', 'IP:127.0.0.1'), ('wrongname', 'IP:10.9.8.7')):
            openssl('req', '-newkey', 'rsa:2048', '-nodes', '-keyout', name + '.key', '-out', name + '.csr', '-subj', '/CN=' + name, cwd=d)
            Path(d, name + '.ext').write_text('subjectAltName=' + san + '\nauthorityKeyIdentifier=keyid\n')
            openssl('x509', '-req', '-in', name + '.csr', '-CA', 'ca.pem', '-CAkey', 'ca.key', '-CAcreateserial', '-out', name + '.pem',
                    '-days', '2', '-extfile', name + '.ext', cwd=d)
        cls.servers = {}
        for name in ('good', 'wrongname'):
            server = QuietServer(('127.0.0.1', 0), Handler)
            context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            context.load_cert_chain(Path(d, name + '.pem'), Path(d, name + '.key'))
            server.socket = context.wrap_socket(server.socket, server_side=True)
            threading.Thread(target=server.serve_forever, daemon=True).start()
            cls.servers[name] = server

    @classmethod
    def tearDownClass(cls):
        for server in cls.servers.values():
            server.shutdown()
            server.server_close()
        cls.dir.cleanup()

    def setUp(self):
        State.version, State.setup, State.register_enabled = VERSION, True, False
        State.script, State.token_seen = b'javascript ' * 20, []

    def probe(self, name, trust_ca, **env):
        base = {k: v for k, v in os.environ.items() if not k.startswith(('AISCOO_PROBE_', 'SSL_CERT'))}
        base.update({'AISCOO_PROBE_ATTEMPTS': '1', 'AISCOO_PROBE_DELAY': '0'})
        if trust_ca:
            base['SSL_CERT_FILE'] = str(Path(self.dir.name, 'ca.pem'))
        else:
            base['SSL_CERT_FILE'] = os.devnull
        base.update(env)
        origin = 'https://127.0.0.1:%d' % self.servers[name].server_port
        return subprocess.run([sys.executable, '-I', str(ROOT / 'probe.py'), origin, SHA], text=True, capture_output=True, env=base)

    def test_trusted_chain_is_verified_by_default(self):
        result = self.probe('good', True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('TLS certificate chain verified', result.stdout)
        self.assertIn('TLS: verified', result.stdout)
        self.assertNotIn('UNVERIFIED', result.stdout)

    def test_untrusted_chain_fails_in_strict_mode(self):
        for mode in ({}, {'AISCOO_PROBE_TLS': 'strict'}):
            with self.subTest(mode=mode):
                result = self.probe('good', False, **mode)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('does not verify', result.stderr)
                self.assertNotIn('ACCEPTANCE', result.stdout)

    def test_untrusted_chain_falls_back_loudly(self):
        result = self.probe('good', False, AISCOO_PROBE_TLS='fallback')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('WARNING: the TLS certificate chain could NOT be verified', result.stdout)
        self.assertIn('TLS: UNVERIFIED', result.stdout)
        self.assertIn('ACCEPTANCE: basic', result.stdout)

    def test_fallback_still_checks_the_version(self):
        State.version = 'release-' + 'b' * 12
        self.assertNotEqual(self.probe('good', False, AISCOO_PROBE_TLS='fallback').returncode, 0)

    def test_unverified_tls_never_receives_the_token(self):
        result = self.probe('good', False, AISCOO_PROBE_TLS='fallback', AISCOO_PROBE_TOKEN='test-token', AISCOO_PROBE_USER_ID='42')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('ACCEPTANCE: basic', result.stdout)
        self.assertEqual(State.token_seen, [])
        self.assertNotIn('test-token', result.stdout + result.stderr)

    def test_verified_tls_may_carry_the_token(self):
        result = self.probe('good', True, AISCOO_PROBE_TOKEN='test-token', AISCOO_PROBE_USER_ID='42')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('ACCEPTANCE: full', result.stdout)

    def test_wrong_hostname_is_never_downgraded(self):
        result = self.probe('wrongname', True, AISCOO_PROBE_TLS='fallback')
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('UNVERIFIED', result.stdout)
        self.assertNotIn('ACCEPTANCE', result.stdout)

    def test_bad_tls_mode_is_rejected(self):
        self.assertNotEqual(self.probe('good', True, AISCOO_PROBE_TLS='off').returncode, 0)


class ReleaseHeadChecks(unittest.TestCase):
    """Run the workflow's release guards locally with an isolated GitHub CLI stub."""

    @classmethod
    def setUpClass(cls):
        try:
            import yaml
        except ImportError:
            raise unittest.SkipTest('PyYAML unavailable')
        cls.jobs = yaml.safe_load((REPO / '.github/workflows/deploy-admin.yml').read_text())['jobs']
        cls.preflight = next(step for step in cls.jobs['deploy']['steps']
                             if step['name'].startswith('Verify artifact digest and release HEAD'))
        cls.postflight = next(step for step in cls.jobs['deploy']['steps']
                              if step['name'] == 'Re-check release HEAD after deploying')

    def run_guard(self, step, latest=SHA, gh_status=0, digest_matches=True):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'artifact').mkdir()
            artifact = b'test deployment artifact'
            (root / 'artifact/aiscoo-admin').write_bytes(artifact)
            gh = root / 'gh'
            gh.write_text('#!/usr/bin/env bash\nprintf "%s\\n" "$TEST_RELEASE_HEAD"\nexit "$TEST_GH_STATUS"\n')
            gh.chmod(0o700)
            output = root / 'output'
            env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ['PATH'],
                       RUNNER_TEMP=str(root), GITHUB_OUTPUT=str(output), GITHUB_SHA=SHA,
                       GITHUB_REPOSITORY='test/repo', TEST_RELEASE_HEAD=latest, TEST_GH_STATUS=str(gh_status),
                       EXPECTED_DIGEST=hashlib.sha256(artifact).hexdigest() if digest_matches else '0' * 64)
            result = subprocess.run(['bash', '-c', step['run']], env=env, text=True,
                                    capture_output=True, timeout=10)
            return result, output.read_text() if output.exists() else ''

    def test_current_release_passes_both_guards(self):
        for step in (self.preflight, self.postflight):
            with self.subTest(step=step['name']):
                result, output = self.run_guard(step)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertNotIn('superseded=true', output)

    def test_newer_release_skips_both_guards_without_failing(self):
        for step in (self.preflight, self.postflight):
            with self.subTest(step=step['name']):
                result, output = self.run_guard(step, latest='b' * 40)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn('superseded=true', output)

    def test_failed_or_invalid_head_lookup_remains_a_failure(self):
        for step in (self.preflight, self.postflight):
            for latest, status in ((SHA, 1), ('null', 0), ('', 0)):
                with self.subTest(step=step['name'], latest=latest, status=status):
                    result, output = self.run_guard(step, latest=latest, gh_status=status)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertNotIn('superseded=true', output)

    def test_digest_mismatch_remains_a_failure_for_an_outdated_release(self):
        result, output = self.run_guard(self.preflight, latest='b' * 40, digest_matches=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Artifact digest differs', result.stderr)
        self.assertNotIn('superseded=true', output)

    def test_superseded_release_gates_upload_probes_and_notifications(self):
        self.assertEqual(self.preflight.get('id'), 'preflight')
        self.assertEqual(self.postflight.get('id'), 'postflight')
        deploy = self.jobs['deploy']
        self.assertEqual(deploy['outputs'].get('superseded'),
                         '${{ steps.preflight.outputs.superseded || steps.postflight.outputs.superseded }}')
        for step in deploy['steps'][3:]:
            with self.subTest(step=step['name']):
                self.assertEqual(step.get('if'), "steps.preflight.outputs.superseded != 'true'")
        self.assertEqual(self.jobs['notify']['if'],
                         "always() && !cancelled() && needs.deploy.outputs.superseded != 'true'")


class Files(unittest.TestCase):
    """Static contracts of the deployment files; the behaviour is covered by test_linux_deploy.sh."""

    def test_no_sudo_or_root_in_deploy_scripts(self):
        for name in ('common.sh', 'receive.sh', 'rollback.sh', 'prestart.sh', 'setup-magpie.sh', 'aiscoo-admin.service'):
            text = (ROOT / name).read_text()
            code = '\n'.join(line for line in text.splitlines() if not line.lstrip().startswith('#'))
            with self.subTest(name=name):
                self.assertIsNone(re.search(r'\bsudo\b|/etc/aiscoo-admin|/opt/aiscoo-admin|/etc/systemd', code))

    def test_user_unit_contract(self):
        unit = (ROOT / 'aiscoo-admin.service').read_text()
        self.assertIn('Restart=no', unit)
        self.assertRegex(unit, r'(?m)^UnsetEnvironment=ENABLE_PPROF CRYPTO_SECRET NODE_NAME$')
        self.assertRegex(unit, r'(?m)^After=aiscoo-xray\.service$')
        self.assertRegex(unit, r'(?m)^Wants=aiscoo-xray\.service$')
        self.assertRegex(unit, r'(?m)^UMask=0077$')
        self.assertIn('Environment=HOME=%h/aiscoo-admin/state', unit)
        self.assertIn('Environment=TMPDIR=%h/aiscoo-admin/state/tmp', unit)
        self.assertIn('Environment=PATH=/usr/local/bin:/usr/bin:/bin', unit)
        self.assertIn('ExecStartPre=%h/aiscoo-admin/bin/prestart.sh', unit)
        self.assertIn('--log-dir %h/aiscoo-admin/state/logs', unit)
        self.assertIn('TimeoutStopSec=130', unit)
        # Directives that need unprivileged user namespaces are deliberately absent (unverified on the host).
        code = '\n'.join(l for l in unit.splitlines() if not l.lstrip().startswith('#'))
        self.assertIsNone(re.search(r'^(ProtectSystem|ProtectHome|PrivateTmp|ReadWritePaths)=', code, re.M))
        for name in ('setup-magpie.sh', 'receive.sh', 'prestart.sh', 'rollback.sh', 'common.sh'):
            body = '\n'.join(l for l in (ROOT / name).read_text().splitlines() if not l.lstrip().startswith('#'))
            self.assertIsNone(re.search(r'systemctl[^\n]*\b(enable|disable)\b', body), name + ' must never enable the service')
        self.assertIn('EnvironmentFile=%h/aiscoo-admin/etc/app.env', unit)
        self.assertIn('--port 3000', unit)
        self.assertIsNone(re.search(r'postgres(ql)?://|SQL_DSN\s*=', unit))

    def test_app_env_tooling_never_prints_values(self):
        text = (ROOT / 'make-app-env.sh').read_text()
        code = '\n'.join(l for l in text.splitlines() if not l.lstrip().startswith('#'))
        self.assertIsNone(re.search(r'\bsudo\b|\bcat\b|set -x|\becho\b', code))
        # Every printf of a variable goes into the file via the { … } > "$tmp" block; the messages never include values.
        for line in code.splitlines():
            message = line[line.index('die '):] if 'die ' in line else line if "printf 'make-app-env" in line else ''
            self.assertNotRegex(message, r'\$(dsn_line|secret)\b')
        common = (ROOT / 'common.sh').read_text()
        body = common[common.index('check_app_env() {'):common.index('wait_postgres() {')]
        self.assertNotRegex(body, r'die [^\n]*\$(\{?value|\{?line\b)')

    def test_workflow_contract(self):
        text = (REPO / '.github/workflows/deploy-admin.yml').read_text()
        for use in re.findall(r'^\s*(?:-\s+)?uses:\s*(\S+)', text, re.M):
            self.assertRegex(use, r'@[0-9a-f]{40}$', 'actions must be pinned to a commit SHA')
        self.assertNotRegex(text, r'\bsudo\b')
        self.assertRegex(text, r'branches:\s*\[release\]')
        self.assertIn('cancel-in-progress: false', text)
        self.assertNotIn('ubuntu-latest', text)
        self.assertEqual(set(re.findall(r'runs-on:\s*(\S+)', text)), {'ubuntu-24.04'})
        self.assertIn('-X \'github.com/QuantumNous/new-api/common.Version=$VERSION\'', text)
        self.assertIn('version=release-%s', text)
        self.assertIn('"${GITHUB_SHA:0:12}"', text)
        self.assertIn('CGO_ENABLED: \'0\'', text)
        self.assertIn('https://112.124.202.60:8443', text)
        self.assertIn('http://112.124.202.60:8080', text)
        self.assertIn('AISCOO_PROBE_NOT_ADMIN_ORIGINS', text)
        self.assertNotIn('ADMIN_TEST_TOKEN', text)
        # Secrets may only be read into env: blocks, never interpolated inside a run script.
        for block in re.findall(r'run:\s*\|?\n((?:[ ]{10,}.*\n?)+)', text):
            self.assertNotIn('secrets.', block)
            self.assertNotIn('${{', block)

    def test_admin_origin_always_carries_8443(self):
        # 443 belongs to the website. The only portless https://112.124.202.60 allowed is the explicit "must NOT be the
        # admin service" check; every other occurrence in the deployment files must be :8443 (or the :8080 test entry).
        files = [REPO / '.github/workflows/deploy-admin.yml', REPO / 'docs/admin-test-deployment.md'] + \
            [p for p in ROOT.iterdir() if p.is_file() and p.name != 'test_deploy.py' and p.suffix != '.pyc'] + \
            [ROOT / 'testdata/linux_inner.sh']
        for path in files:
            for number, line in enumerate(path.read_text().splitlines(), 1):
                for match in re.finditer(r'https://112\.124\.202\.60(?!:8443)', line):
                    allowed = ('NOT_ADMIN' in line or '不得' in line or '443' in line or '官网' in line or 'website' in line.lower())
                    self.assertTrue(allowed, '%s:%d has a portless admin origin: %s' % (path.name, number, line.strip()))
        self.assertNotIn('112.124.202.60:443', (REPO / '.github/workflows/deploy-admin.yml').read_text())
        env = (ROOT / 'make-app-env.sh').read_text()
        for key in ('XCODE_PUBLIC_ORIGIN', 'SESSION_COOKIE_TRUSTED_URL'):
            self.assertIn("'%s=https://112.124.202.60:8443'" % key, env)

    def test_entry_hash_covers_the_installed_scripts(self):
        script = (ROOT / 'entry-hash.sh').read_text()
        for name in ('entry-hash.sh', 'common.sh', 'receive.sh', 'rollback.sh', 'prestart.sh', 'aiscoo-admin.service'):
            self.assertIn(name, script)
        setup = (ROOT / 'setup-magpie.sh').read_text()
        self.assertIn('common.sh receive.sh rollback.sh prestart.sh entry-hash.sh', setup)
        self.assertIn('entry-hash.sh scripts/deploy scripts/deploy/aiscoo-admin.service',
                      (REPO / '.github/workflows/deploy-admin.yml').read_text())

    def test_workflow_structure_and_notification_rules(self):
        try:
            import yaml
        except ImportError:
            self.skipTest('PyYAML unavailable')
        data = yaml.safe_load((REPO / '.github/workflows/deploy-admin.yml').read_text())
        jobs = data['jobs']
        self.assertEqual(set(jobs), {'build', 'deploy', 'notify'})
        self.assertEqual(jobs['deploy']['needs'], 'build')
        self.assertEqual(jobs['notify']['needs'], ['build', 'deploy'])
        self.assertEqual(jobs['notify']['if'],
                         "always() && !cancelled() && needs.deploy.outputs.superseded != 'true'")
        for name in ('build', 'deploy'):
            self.assertIn('refs/heads/release', jobs[name]['if'])
        self.assertEqual(set(data[True]), {'push', 'workflow_dispatch'})
        self.assertNotIn('environment', jobs['deploy'], 'environments need a plan this repository may not have')
        run = jobs['notify']['steps'][0]['run']
        self.assertIn('exit 0', run.splitlines()[-1], 'a notification problem must never fail the workflow')
        success = re.search(r'if \[\[(.*?)\]\]; then\n\s*text="[^"]*成功', run, re.S)
        self.assertIsNotNone(success)
        for needed in ('BUILD_RESULT == success', 'DEPLOY_RESULT == success', 'ACCEPTANCE == full', 'ACCEPTANCE == basic'):
            self.assertIn(needed, success.group(1))
        steps = [step.get('name', '') for step in jobs['deploy']['steps']]
        self.assertLess(steps.index('Public acceptance probe'), steps.index('Re-check release HEAD after deploying'))
        by_id = {s.get('id'): s for s in jobs['deploy']['steps'] if s.get('id')}
        self.assertTrue(by_id['http_probe']['continue-on-error'], '8080 is supplementary and reported, not blocking')
        self.assertNotIn('continue-on-error', by_id['probe'], '8443 acceptance must block')
        self.assertTrue(steps[2].startswith('Verify artifact digest and release HEAD'))


if __name__ == '__main__':
    unittest.main()
