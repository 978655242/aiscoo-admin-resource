#!/usr/bin/env python3
"""Create or update the four aiscoo subscription plan templates (Lite/Standard/Pro/Max).

Dry run by default. Pass --apply to write. The admin access token is read from
AISCOO_ADMIN_TOKEN (needs billing:read and billing:write scopes) and is never printed.
Re-running updates the plans in place, matched by xcode_display.group_key.

    AISCOO_ADMIN_TOKEN=... python3 scripts/seed_subscription_plans.py --apply
"""

import argparse
import json
import os
import sys
import urllib.error
import urllib.request

# 1 point = CNY 1; the site converts at USD 1 = CNY 7.3 and USD 1 = 500000 quota.
CNY_PER_USD = 7.3
QUOTA_PER_USD = 500_000

FEATURES = [
    "aiscoo 官方模型",
    "aiscoo 桌面端 Agent",
    "统一额度与用量管理",
    "额度仅限 aiscoo 客户端使用，不扣钱包余额",
    "一次性开通，不自动续费",
]

# (group_key, title, price CNY, days, credited points, bonus points, sort_order, description)
TIERS = [
    ("lite", "Lite", 30, 31, 30, 0, 40, "适合体验 aiscoo 与轻度日常对话"),
    ("standard", "Standard", 100, 62, 105, 5, 30, "适合日常对话、开发辅助与偶尔的 Agent 任务"),
    ("pro", "Pro", 500, 120, 530, 30, 20, "适合高频开发工作流与密集 Agent 任务"),
    ("max", "Max", 1000, 180, 1100, 100, 10, "适合重度使用与更高额度的长期工作流"),
]


def build_plan(tier):
    key, title, price, days, points, bonus, sort_order, description = tier
    credited = f"到账 {points} 积分" + (f"（含赠送 {bonus} 积分）" if bonus else "")
    return {
        "title": title,
        "subtitle": description,
        "price_amount": price,
        "currency": "CNY",
        "duration_unit": "day",
        "duration_value": days,
        "enabled": True,
        "sort_order": sort_order,
        "allow_balance_pay": False,
        "max_purchase_per_user": 0,
        "total_amount": round(points / CNY_PER_USD * QUOTA_PER_USD),
        "quota_reset_period": "never",
        "xcode_display": {
            "group_key": key,
            "group_title": title,
            "offer_kind": "standard",
            "highlights": [credited, f"{days} 天有效"],
            "features": FEATURES,
        },
    }


def call(base, token, method, path, body=None):
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(
        base + path,
        data=data,
        method=method,
        headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(request, timeout=20) as response:
            result = json.load(response)
    except urllib.error.HTTPError as error:
        sys.exit(f"{method} {path} -> HTTP {error.code}: {error.read().decode()[:300]}")
    if not result.get("success"):
        sys.exit(f"{method} {path} failed: {result.get('message')}")
    return result.get("data")


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--apply", action="store_true", help="write to the server")
    parser.add_argument("--base", default=os.environ.get("AISCOO_ADMIN_URL", "http://112.124.202.60:8080"))
    args = parser.parse_args()

    plans = [build_plan(tier) for tier in TIERS]
    if not args.apply:
        print(json.dumps(plans, ensure_ascii=False, indent=2))
        print("\ndry run: nothing sent. Re-run with --apply to write.")
        return

    token = os.environ.get("AISCOO_ADMIN_TOKEN", "").strip()
    if not token:
        sys.exit("set AISCOO_ADMIN_TOKEN")

    existing = {}
    for item in call(args.base, token, "GET", "/api/subscription/admin/plans") or []:
        display = (item.get("plan") or {}).get("xcode_display") or {}
        if display.get("group_key"):
            existing[display["group_key"]] = item["plan"]["id"]

    for plan in plans:
        key = plan["xcode_display"]["group_key"]
        if key in existing:
            call(args.base, token, "PUT", f"/api/subscription/admin/plans/{existing[key]}", {"plan": plan})
            print(f"updated {plan['title']} (id {existing[key]})")
        else:
            created = call(args.base, token, "POST", "/api/subscription/admin/plans", {"plan": plan})
            print(f"created {plan['title']} (id {created['id']})")

    stored = {
        (item["plan"].get("xcode_display") or {}).get("group_key"): item["plan"]
        for item in call(args.base, token, "GET", "/api/subscription/admin/plans")
    }
    for plan in plans:
        key = plan["xcode_display"]["group_key"]
        got = stored.get(key)
        if got is None:
            sys.exit(f"read-back: {plan['title']} is missing")
        for field in ("price_amount", "currency", "duration_unit", "duration_value", "total_amount", "enabled"):
            if got[field] != plan[field]:
                sys.exit(f"read-back: {plan['title']}.{field} is {got[field]!r}, expected {plan[field]!r}")
        if got["xcode_display"] != plan["xcode_display"]:
            sys.exit(f"read-back: {plan['title']}.xcode_display differs")
    print("read-back OK for all 4 plans")


if __name__ == "__main__":
    main()
