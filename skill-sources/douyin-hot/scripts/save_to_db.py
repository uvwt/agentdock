#!/usr/bin/env python3
"""
Douyin Hot - Save to Database
将抖音热榜保存到数据库
"""

import subprocess
import json
import sys
from pathlib import Path
from datetime import datetime, timezone

sys.path.insert(0, str(Path(__file__).parent))
from db import save_hot_items, log_fetch

def fetch_and_save(limit=50):
    """采集抖音热榜并保存到数据库"""
    script_dir = Path(__file__).parent
    node_script = script_dir / "douyin_json.js"  # 使用JSON输出版本
    
    print("🔥 采集抖音热榜...")
    
    try:
        # 调用Node.js脚本获取数据
        result = subprocess.run(
            ['node', str(node_script), str(limit)],
            capture_output=True,
            text=True,
            timeout=30
        )
        
        if result.returncode != 0:
            print(f"❌ 脚本执行失败: {result.stderr}")
            log_fetch(0, 0, status=2, error=result.stderr)
            return None
        
        # 解析JSON输出
        items = json.loads(result.stdout)
        
        if isinstance(items, dict) and items.get('error'):
            print(f"❌ 获取失败: {items['error']}")
            log_fetch(0, 0, status=2, error=items['error'])
            return None
        
        print(f"   获取到 {len(items)} 条热榜")
        
        # 保存到数据库
        result = save_hot_items(items)
        
        # 记录日志
        status = 0 if result['errors'] == 0 else 1
        log_fetch(result['found'], result['new'], status=status)
        
        return result
        
    except json.JSONDecodeError as e:
        print(f"❌ JSON解析失败: {e}")
        log_fetch(0, 0, status=2, error=f'JSON解析失败: {e}')
        return None
    except subprocess.TimeoutExpired:
        print("❌ 采集超时")
        log_fetch(0, 0, status=2, error='Timeout')
        return None
    except Exception as e:
        print(f"❌ 异常: {e}")
        log_fetch(0, 0, status=2, error=str(e))
        return None

def main():
    """主函数"""
    args = sys.argv[1:]
    limit = int(args[0]) if args else 50
    
    print("=" * 60)
    print(f"🎵 抖音热榜采集 → 数据库")
    print(f"   数量: {limit}")
    print("=" * 60)
    
    # 确保数据库已初始化
    db_path = Path(__file__).parent.parent / "data" / "douyin.db"
    if not db_path.exists():
        print("\n🔄 初始化数据库...")
        from init_db import init_db
        init_db()
    
    # 采集数据
    result = fetch_and_save(limit)
    
    if result:
        print("\n" + "=" * 60)
        print("✅ 完成!")
        print(f"   数据库: {db_path}")
        print(f"   发现: {result['found']} 条")
        print(f"   新增: {result['new']} 条")
        if result['errors'] > 0:
            print(f"   错误: {result['errors']} 条")
        
        # 显示Top 5
        print("\n🔥 Top 5:")
        from db import get_items_by_date
        items = get_items_by_date(limit=5)
        for item in items:
            label_str = f" [{item['label']}]" if item['label'] else ""
            pop_str = f"{item['popularity']:,}" if item['popularity'] else "N/A"
            print(f"  {item['rank']}. {item['title'][:40]}...{label_str}")
            print(f"     热度: {pop_str}")
    else:
        print("\n❌ 采集失败")
        sys.exit(1)

if __name__ == '__main__':
    main()
