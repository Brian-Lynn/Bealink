#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import os
import sys
import time
from pathlib import Path

def choose_scan_path() -> Path:
    """让用户选择扫描目录"""
    script_dir = Path(__file__).parent.resolve()
    cwd = Path.cwd().resolve()

    print("请选择要扫描的目录：")
    print("  1) 当前命令行所在路径：", cwd)
    print("  2) 脚本所在路径：      ", script_dir)
    print("  3) 自定义路径")
    choice = input("输入选项 [1-3] 并回车：").strip()

    if choice == "1":
        return cwd
    if choice == "2":
        return script_dir
    if choice == "3":
        custom = input("请输入自定义路径：").strip().strip('"').strip("'")
        p = Path(custom).expanduser().resolve()
        if not p.is_dir():
            print(f"错误：路径 {p} 不存在或不是目录。")
            sys.exit(1)
        return p

    print("无效选项，退出。")
    sys.exit(1)

def format_mtime(ts: float) -> str:
    """用 time.localtime 避免 Windows 上 timestamp 越界"""
    try:
        return time.strftime("%Y-%m-%d %H:%M:%S", time.localtime(ts))
    except Exception:
        return "1970-01-01 00:00:00"

def build_tree(root: Path) -> dict:
    """
    构建一个树形结构：
    node = { 'dirs': { dirname: node, … }, 'files': [ {name, size, mtime}, … ] }
    """
    tree = { 'dirs': {}, 'files': [] }
    for dirpath, _, filenames in os.walk(root):
        rel = Path(dirpath).relative_to(root)
        node = tree
        # 逐层进入子目录
        for part in rel.parts:
            node = node['dirs'].setdefault(part, { 'dirs': {}, 'files': [] })
        # 收集当前目录下的文件信息
        for fname in filenames:
            fp = Path(dirpath) / fname
            try:
                st = fp.stat()
            except Exception:
                continue
            node['files'].append({
                'name': fname,
                'size': st.st_size,
                'mtime': format_mtime(st.st_mtime)
            })
    return tree

def write_tree_md(root: Path, tree: dict):
    """把树形结构写入 Markdown 文件"""
    md = root / "file_tree.md"
    with md.open("w", encoding="utf-8") as f:
        f.write(f"# 文件树：{root}\n\n")
        f.write("```text\n")
        f.write(f"{root}\n")

        def _dump(node: dict, prefix: str):
            # 先写子目录
            dirs = list(node['dirs'].items())
            for idx, (dname, sub) in enumerate(dirs):
                is_last = idx == len(dirs) - 1 and not node['files']
                conn = "└── " if is_last else "├── "
                f.write(f"{prefix}{conn}{dname}/\n")
                new_pref = prefix + ("    " if is_last else "│   ")
                _dump(sub, new_pref)

            # 再写同级文件
            for idx, info in enumerate(node['files']):
                is_last = idx == len(node['files']) - 1
                conn = "└── " if is_last else "├── "
                f.write(f"{prefix}{conn}{info['name']} "
                        f"({info['size']} bytes, {info['mtime']})\n")

        _dump(tree, "")
        f.write("```\n")

    print(f"\n✅ 完成，已生成：{md}\n")

def main():
    scan_path = choose_scan_path()
    print(f"\n正在扫描：{scan_path} …\n")
    tree = build_tree(scan_path)
    write_tree_md(scan_path, tree)

if __name__ == "__main__":
    main()