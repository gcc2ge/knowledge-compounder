#!/usr/bin/env python3
"""
observe.py — 快速捕获策略执行中的异常观察，自动写入 wiki 观察日志。

Usage:
  observe                                      # 交互式捕获（引导填关键信息）
  observe -s sniping -t "title" -c "body"      # 直接捕获（适合 pipe 或 alias）
  observe -s arbitrage -t "title" -b            # 从 stdin 读 body
  observe list                                  # 列出最近未编译的观察
  observe --help                                # 帮助

输出: raw/observations/YYYY-MM-DD-HHMMSS-slug.md

闭环:
  observe 写入 → raw/observations/ 文件被 compiler 扫描 → 编译进 wiki
  (或: observe --compile 直接启动 compiler 处理该观察)
"""

import argparse
import json
import os
import re
import sys
import textwrap
from datetime import datetime, timezone
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
RAW_DIR = REPO_ROOT / "raw"
OBS_DIR = RAW_DIR / "observations"
WIKI_DIR = REPO_ROOT / "wiki"

STRATEGIES = ["sniping", "arbitrage", "grid", "market-making", "analysis", "other"]
TYPES = ["anomaly", "pattern", "signal", "hypothesis", "failure", "edge", "other"]


def slugify(text: str) -> str:
    """生成文件名友好的 slug（中文保留，特殊字符替换）"""
    s = text.strip().lower()
    # 替换空格和特殊字符为连字符
    s = re.sub(r'[^\w一-鿿\-]', '-', s)
    s = re.sub(r'-+', '-', s)
    s = s.strip('-')
    # 截断
    if len(s) > 60:
        s = s[:60]
    return s


def timestamp() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S+00:00")


def local_ts() -> str:
    """本地时间用于文件名"""
    return datetime.now().strftime("%Y-%m-%d-%H%M%S")


def prompt_input(prompt: str, default: str = "") -> str:
    """带默认值的输入提示"""
    if default:
        val = input(f"{prompt} [{default}]: ").strip()
        return val if val else default
    return input(f"{prompt}: ").strip()


def prompt_multiline(prompt: str, end_marker: str = "---") -> str:
    """多行输入，直到输入 end_marker 结束"""
    print(f"{prompt}（输入 '{end_marker}' 结束）:")
    lines = []
    while True:
        line = input()
        if line.strip() == end_marker:
            break
        lines.append(line)
    return "\n".join(lines)


def write_observation(args) -> Path:
    """写入观察文件"""
    # 确保目录存在
    OBS_DIR.mkdir(parents=True, exist_ok=True)

    # 收集数据
    if args.interactive:
        data = collect_interactive()
    else:
        data = {
            "strategy": args.strategy or "other",
            "type": args.type or "anomaly",
            "title": args.title or "未命名观察",
            "body": args.body or "",
            "tags": args.tags or [],
            "confidence": args.confidence or "low",
        }
        # 如果 -b 标记且 stdin 有数据，追加 body
        if args.stdin_body and not sys.stdin.isatty():
            stdin_data = sys.stdin.read().strip()
            if stdin_data:
                if data["body"]:
                    data["body"] += "\n\n" + stdin_data
                else:
                    data["body"] = stdin_data

    # 生成文件名
    ts = local_ts()
    slug = slugify(data["title"])
    filename = f"{ts}-{slug}.md"
    filepath = OBS_DIR / filename

    # 构造 frontmatter
    now_ts = timestamp()
    frontmatter = {
        "type": "observation",
        "strategy": data["strategy"],
        "observation_type": data["type"],
        "observed_at": now_ts,
        "confidence": data["confidence"],
        "compiled": False,
        "tags": data["tags"] if data["tags"] else [],
    }

    # 格式化 frontmatter
    fm_lines = ["---"]
    for key, val in frontmatter.items():
        if isinstance(val, list):
            if val:
                fm_lines.append(f"{key}:")
                for v in val:
                    fm_lines.append(f"  - {v}")
            else:
                fm_lines.append(f"{key}: []")
        elif isinstance(val, bool):
            fm_lines.append(f"{key}: {'true' if val else 'false'}")
        else:
            fm_lines.append(f"{key}: {val}")
    fm_lines.append("---")

    # 构造正文
    body_lines = [
        f"# {data['title']}",
        "",
        "## 发生了什么",
        "",
        data["body"] if data["body"] else "待补充",
        "",
        "## 我的假设",
        "",
        "> 我认为这可能意味着什么？",
        "",
        "## 需要验证",
        "",
        "- [ ] 回溯数据确认模式",
        "- [ ] 检查是否有其他类似案例",
        "- [ ] 与已有 wiki 知识对比",
        "- [ ] 如果 confirmed → 编译进 wiki",
        "",
        "## 上下文",
        "",
        f"- 策略: {data['strategy']}",
        f"- 观察类型: {data['type']}",
        f"- 时间: {now_ts}",
        "",
    ]

    content = "\n".join(fm_lines + [""] + body_lines)
    filepath.write_text(content, encoding="utf-8")

    # 可选：追加到 log.md
    if args.log:
        append_log(data["title"], data["strategy"], str(filepath.relative_to(REPO_ROOT)))

    return filepath


def collect_interactive() -> dict:
    """交互式引导收集"""
    print("\n" + "=" * 50)
    print("  📝 Wiki 观察捕获")
    print("=" * 50 + "\n")

    strategy = prompt_input("策略类型", "sniping")
    if strategy not in STRATEGIES:
        print(f"  ⚠️  未知策略类型，可选: {', '.join(STRATEGIES)}")
        strategy = prompt_input("策略类型", strategy)

    obs_type = prompt_input("观察类型", "anomaly")
    if obs_type not in TYPES:
        print(f"  ⚠️  未知观察类型，可选: {', '.join(TYPES)}")
        obs_type = prompt_input("观察类型", obs_type)

    title = prompt_input("标题（一句话概括你的发现）")
    while not title:
        title = prompt_input("标题不能为空")

    print()
    print("描述发生了什么（多行，输入 '---' 结束）:")
    print("  > 例如：在 Pons V2 上发现一个创建者 creatorTaxBps=0 但 feeBps=800，")
    print("  > 此前的经验是 creatorTaxBps 高才 rug，但这个组合可能是一个新模式。")
    print("  > 尝试了 3 次，2 次归零，1 次 -50%。")
    body = prompt_multiline("")

    tags_str = prompt_input("标签（逗号分隔，可选）", "")
    tags = [t.strip() for t in tags_str.split(",") if t.strip()]

    confidence = prompt_input("置信度 (low/medium/high)", "low")

    print()
    return {
        "strategy": strategy,
        "type": obs_type,
        "title": title,
        "body": body,
        "tags": tags,
        "confidence": confidence,
    }


def append_log(title: str, strategy: str, filepath: str):
    """追加到 log.md"""
    log_path = REPO_ROOT / "log.md"
    now = datetime.now().strftime("%Y-%m-%d %H:%M")
    entry = (
        f"\n## [{now}] observe | {title}\n"
        f"- 操作: observe\n"
        f"- 描述: 策略 [{strategy}] 执行中捕获观察\n"
        f"- 涉及页面: [[{filepath}]]\n"
    )
    with open(log_path, "a", encoding="utf-8") as f:
        f.write(entry)


def cmd_list():
    """列出未编译的观察"""
    OBS_DIR.mkdir(parents=True, exist_ok=True)
    files = sorted(OBS_DIR.glob("*.md"), reverse=True)

    if not files:
        print("raw/observations/ 中没有观察记录")
        return

    # 显示未编译的
    uncompiled = []
    compiled = []
    for f in files:
        content = f.read_text(encoding="utf-8")
        if "compiled: false" in content or "compiled: False" in content:
            uncompiled.append(f)
        else:
            compiled.append(f)

    if uncompiled:
        print(f"\n📋 未编译观察 ({len(uncompiled)}):")
        print("-" * 60)
        for f in uncompiled[:15]:
            # 提取标题
            content = f.read_text(encoding="utf-8")
            title_match = re.search(r"^# (.+)$", content, re.MULTILINE)
            title = title_match.group(1) if title_match else f.stem
            strategy_match = re.search(r"strategy: (\w+)", content)
            strategy = strategy_match.group(1) if strategy_match else "?"
            ctime = datetime.fromtimestamp(f.stat().st_mtime).strftime("%m-%d %H:%M")
            rel = f.relative_to(REPO_ROOT)
            print(f"  [{ctime}] [{strategy}] {title}")
            print(f"          {rel}")
    else:
        print("  所有观察均已编译 ✅")

    if compiled:
        print(f"\n✅ 已编译观察 ({len(compiled)})")
        if compiled:
            print(f"  最近: {compiled[-1].relative_to(REPO_ROOT)}")


def cmd_compile_one(obs_path: Path):
    """标记一个观察为已编译"""
    if not obs_path.exists():
        print(f"Error: {obs_path} 不存在")
        sys.exit(1)

    content = obs_path.read_text(encoding="utf-8")
    # 将 compiled: false 改为 compiled: true
    new_content = re.sub(r'compiled:\s*(false|False)', 'compiled: true', content)
    if new_content != content:
        obs_path.write_text(new_content, encoding="utf-8")
        print(f"✅ 标记为已编译: {obs_path.relative_to(REPO_ROOT)}")
    else:
        print(f"⚠️  未找到 compiled: false 标记，可能已编译或格式异常")


def main():
    parser = argparse.ArgumentParser(
        description="Wiki 观察捕获工具 — 快速记录策略执行中的异常发现",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=textwrap.dedent("""\
            示例:
              observe                          # 交互式捕获
              observe -s sniping -t "标题" -c "内容"  # 直接捕获
              echo "异常数据" | observe -s arbitrage -t "标题" -b  # 从 stdin 读
              observe list                     # 查看未编译观察
              observe mark-compiled path       # 标记为已编译
        """),
    )

    # 子命令
    subparsers = parser.add_subparsers(dest="command")

    # list 子命令
    list_parser = subparsers.add_parser("list", help="列出最近观察")

    # mark-compiled 子命令
    mc_parser = subparsers.add_parser("mark-compiled", help="标记观察为已编译")
    mc_parser.add_argument("path", help="观察文件路径 (相对 repo 或绝对)")

    # 捕获参数（主命令）
    parser.add_argument("-s", "--strategy", choices=STRATEGIES, help=f"策略类型: {', '.join(STRATEGIES)}")
    parser.add_argument("-t", "--type", choices=TYPES, help=f"观察类型: {', '.join(TYPES)}")
    parser.add_argument("--title", "-T", help="观察标题")
    parser.add_argument("--body", "-c", help="观察内容")
    parser.add_argument("-b", "--stdin-body", action="store_true", help="从 stdin 读取 body 追加")
    parser.add_argument("--tags", nargs="*", default=[], help="标签")
    parser.add_argument("--confidence", choices=["low", "medium", "high"], default="low", help="置信度")
    parser.add_argument("--no-log", action="store_true", help="不追加 log.md")
    parser.add_argument("--interactive", "-i", action="store_true", help="强制交互模式")

    args = parser.parse_args()

    # 处理子命令
    if args.command == "list":
        cmd_list()
        return

    if args.command == "mark-compiled":
        obs_path = Path(args.path)
        if not obs_path.is_absolute():
            obs_path = REPO_ROOT / obs_path
        cmd_compile_one(obs_path)
        return

    # 主命令：捕获观察
    # 交互模式：如果没有提供必要参数，或者显式 --interactive
    is_interactive = args.interactive or not (args.title or args.body)

    if is_interactive:
        args.interactive = True
    else:
        args.interactive = False

    filepath = write_observation(args)

    rel_path = filepath.relative_to(REPO_ROOT)
    print(f"\n✅ 观察已写入: {rel_path}")
    print(f"   下一步: 运行 compiler 编译进 wiki，或继续积累更多观察")
    print(f"   → python3 scripts/compiler-preflight.py {rel_path}")


if __name__ == "__main__":
    main()