#!/usr/bin/env python3
"""
Douyin Hot - Database Initialization
抖音热榜数据库初始化
"""

import sqlite3
from pathlib import Path

DB_PATH = Path(__file__).parent.parent / "data" / "douyin.db"

def init_db():
    """初始化数据库"""
    DB_PATH.parent.mkdir(parents=True, exist_ok=True)
    
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    
    # 热榜条目表
    cursor.execute('''
        CREATE TABLE IF NOT EXISTS hot_items (
            id TEXT PRIMARY KEY,              -- 条目ID: 标题+日期的哈希
            platform TEXT DEFAULT 'douyin',   -- 平台标识
            rank INTEGER,                     -- 排名
            title TEXT NOT NULL,              -- 标题
            popularity INTEGER,               -- 热度值
            link TEXT,                        -- 链接
            label TEXT,                       -- 标签
            item_type TEXT,                   -- 类型
            fetched_at INTEGER DEFAULT (strftime('%s', 'now')), -- 抓取时间
            fetch_date TEXT,                  -- 抓取日期(YYYY-MM-DD)
            raw_data TEXT                     -- 原始JSON
        )
    ''')
    
    # 创建索引
    cursor.execute('CREATE INDEX IF NOT EXISTS idx_hot_items_date ON hot_items(fetch_date)')
    cursor.execute('CREATE INDEX IF NOT EXISTS idx_hot_items_rank ON hot_items(rank)')
    cursor.execute('CREATE INDEX IF NOT EXISTS idx_hot_items_label ON hot_items(label)')
    cursor.execute('CREATE INDEX IF NOT EXISTS idx_hot_items_fetched ON hot_items(fetched_at)')
    
    # 抓取日志表
    cursor.execute('''
        CREATE TABLE IF NOT EXISTS fetch_logs (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            started_at INTEGER DEFAULT (strftime('%s', 'now')),
            ended_at INTEGER,
            found INTEGER DEFAULT 0,
            new INTEGER DEFAULT 0,
            status INTEGER DEFAULT 0,
            error TEXT
        )
    ''')
    
    cursor.execute('CREATE INDEX IF NOT EXISTS idx_fetch_logs_date ON fetch_logs(started_at)')
    
    conn.commit()
    conn.close()
    
    print(f"✅ Douyin database initialized: {DB_PATH}")
    return DB_PATH

if __name__ == '__main__':
    init_db()
