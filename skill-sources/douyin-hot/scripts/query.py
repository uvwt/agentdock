#!/usr/bin/env python3
"""
Douyin Hot - Query Tool
抖音热榜数据查询工具
"""

import sqlite3
from datetime import datetime, timezone
from pathlib import Path
import sys

DB_PATH = Path(__file__).parent.parent / "data" / "douyin.db"

def query_today(limit=50):
    """查询今天的数据"""
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    
    today = datetime.now(timezone.utc).strftime('%Y-%m-%d')
    
    cursor.execute('''
        SELECT rank, title, popularity, link, label
        FROM hot_items
        WHERE fetch_date = ?
        ORDER BY rank ASC
        LIMIT ?
    ''', (today, limit))
    
    rows = cursor.fetchall()
    conn.close()
    
    print(f"\n🎵 抖音热榜 - {today}")
    print("=" * 80)
    
    for row in rows:
        rank, title, popularity, link, label = row
        label_str = f" [{label}]" if label else ""
        pop_str = f"🔥 {popularity:,}" if popularity else ""
        print(f"  {rank:2d}. {title}{label_str}")
        if pop_str:
            print(f"      {pop_str}")
    
    print(f"\n   共 {len(rows)} 条数据")

def query_by_date(date_str, limit=50):
    """查询指定日期的数据"""
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    
    cursor.execute('''
        SELECT rank, title, popularity, link, label
        FROM hot_items
        WHERE fetch_date = ?
        ORDER BY rank ASC
        LIMIT ?
    ''', (date_str, limit))
    
    rows = cursor.fetchall()
    conn.close()
    
    if not rows:
        print(f"❌ 未找到 {date_str} 的数据")
        return
    
    print(f"\n🎵 抖音热榜 - {date_str}")
    print("=" * 80)
    
    for row in rows:
        rank, title, popularity, link, label = row
        label_str = f" [{label}]" if label else ""
        pop_str = f"🔥 {popularity:,}" if popularity else ""
        print(f"  {rank:2d}. {title}{label_str}")
        if pop_str:
            print(f"      {pop_str}")
    
    print(f"\n   共 {len(rows)} 条数据")

def query_stats(days=7):
    """查询统计"""
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    
    cursor.execute('''
        SELECT fetch_date, COUNT(*) as count, 
               SUM(popularity) as total_popularity
        FROM hot_items
        WHERE fetch_date >= date('now', '-{} days')
        GROUP BY fetch_date
        ORDER BY fetch_date DESC
    '''.format(days))
    
    rows = cursor.fetchall()
    conn.close()
    
    print(f"\n📊 最近{days}天统计")
    print("=" * 70)
    print(f"{'日期':<12} {'数量':<10} {'总热度':<15}")
    print("-" * 70)
    
    for row in rows:
        date_str, count, total_pop = row
        total_pop_str = f"{total_pop:,}" if total_pop else "0"
        print(f"{date_str:<12} {count:<10} {total_pop_str:<15}")

def query_logs(limit=10):
    """查询抓取日志"""
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    
    cursor.execute('''
        SELECT started_at, found, new, status
        FROM fetch_logs
        ORDER BY started_at DESC
        LIMIT ?
    ''', (limit,))
    
    rows = cursor.fetchall()
    conn.close()
    
    print(f"\n📝 最近{limit}次抓取日志")
    print("=" * 60)
    print(f"{'时间':<20} {'发现':<8} {'新增':<8} {'状态':<8}")
    print("-" * 60)
    
    for row in rows:
        started, found, new, status = row
        time_str = datetime.fromtimestamp(started, tz=timezone.utc).strftime('%m-%d %H:%M:%S')
        status_str = '✅' if status == 0 else '⚠️' if status == 1 else '❌'
        print(f"{time_str:<20} {found:<8} {new:<8} {status_str:<8}")

def main():
    args = sys.argv[1:]
    
    if not args or args[0] == 'today':
        limit = int(args[1]) if len(args) > 1 else 50
        query_today(limit)
    elif args[0] == 'date':
        if len(args) < 2:
            print("Usage: query.py date YYYY-MM-DD [limit]")
            return
        date_str = args[1]
        limit = int(args[2]) if len(args) > 2 else 50
        query_by_date(date_str, limit)
    elif args[0] == 'stats':
        days = int(args[1]) if len(args) > 1 else 7
        query_stats(days)
    elif args[0] == 'logs':
        limit = int(args[1]) if len(args) > 1 else 10
        query_logs(limit)
    else:
        print("Usage: query.py [command] [args]")
        print("")
        print("Commands:")
        print("  today [limit]           查询今天的数据")
        print("  date YYYY-MM-DD [limit] 查询指定日期的数据")
        print("  stats [days]            统计")
        print("  logs [limit]            抓取日志")

if __name__ == '__main__':
    main()
