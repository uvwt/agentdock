#!/usr/bin/env python3
"""
Douyin Hot - Database Operations
抖音热榜数据库操作模块
"""

import sqlite3
import json
import hashlib
from datetime import datetime, timezone
from pathlib import Path

DB_PATH = Path(__file__).parent.parent / "data" / "douyin.db"

def get_db():
    """获取数据库连接"""
    return sqlite3.connect(DB_PATH)

def generate_id(title, date_str):
    """生成条目ID"""
    return hashlib.sha256((title + date_str).encode()).hexdigest()[:16]

def get_today_str():
    """获取今天的日期字符串"""
    return datetime.now(timezone.utc).strftime('%Y-%m-%d')

def save_hot_items(items):
    """
    保存热榜条目到数据库
    
    Args:
        items: 条目列表
    
    Returns:
        dict: {found, new, errors}
    """
    if not items:
        return {'found': 0, 'new': 0, 'errors': 0}
    
    conn = get_db()
    cursor = conn.cursor()
    
    today = get_today_str()
    now = int(datetime.now(timezone.utc).timestamp())
    
    found = len(items)
    new_count = 0
    errors = 0
    
    for item in items:
        try:
            title = item.get('title', '')
            if not title:
                continue
            
            # 生成唯一ID
            item_id = generate_id(title, today)
            rank = item.get('rank', 0)
            popularity = item.get('popularity', 0)
            link = item.get('link', '')
            label = item.get('label', '')
            item_type = item.get('type', '未知')
            
            # 检查是否已存在
            cursor.execute('SELECT id FROM hot_items WHERE id = ?', (item_id,))
            if cursor.fetchone():
                continue  # 已存在，跳过
            
            # 插入新记录
            cursor.execute('''
                INSERT INTO hot_items 
                (id, platform, rank, title, popularity, link, label, item_type, fetched_at, fetch_date, raw_data)
                VALUES (?, 'douyin', ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ''', (item_id, rank, title, popularity, link, label, item_type, now, today, json.dumps(item, ensure_ascii=False)))
            
            new_count += 1
            
        except Exception as e:
            print(f"   ⚠️ 保存失败: {e}")
            errors += 1
            continue
    
    conn.commit()
    conn.close()
    
    return {
        'found': found,
        'new': new_count,
        'errors': errors
    }

def log_fetch(found, new, status=0, error=None):
    """记录抓取日志"""
    conn = get_db()
    cursor = conn.cursor()
    
    now = int(datetime.now(timezone.utc).timestamp())
    
    cursor.execute('''
        INSERT INTO fetch_logs 
        (started_at, ended_at, found, new, status, error)
        VALUES (?, ?, ?, ?, ?, ?)
    ''', (now, now, found, new, status, error))
    
    conn.commit()
    conn.close()

def get_items_by_date(date_str=None, limit=100):
    """获取指定日期的热榜条目"""
    if date_str is None:
        date_str = get_today_str()
    
    conn = get_db()
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
    
    return [{
        'rank': row[0],
        'title': row[1],
        'popularity': row[2],
        'link': row[3],
        'label': row[4]
    } for row in rows]

def get_stats(days=7):
    """获取最近N天的统计信息"""
    conn = get_db()
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
    
    return [{
        'date': row[0],
        'count': row[1],
        'total_popularity': row[2]
    } for row in rows]

if __name__ == '__main__':
    print(f"Database path: {DB_PATH}")
