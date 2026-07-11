#!/usr/bin/env node

/**
 * 抖音热榜抓取脚本
 * 获取抖音热搜榜数据
 */

const https = require('https');

const USER_AGENTS = [
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0'
];

function getRandomUserAgent() {
  return USER_AGENTS[Math.floor(Math.random() * USER_AGENTS.length)];
}

function fetchDouyinHotBoard() {
  return new Promise((resolve, reject) => {
    const options = {
      hostname: 'www.douyin.com',
      path: '/aweme/v1/hot/search/list/',
      method: 'GET',
      headers: {
        'User-Agent': getRandomUserAgent(),
        'Accept': 'application/json',
        'Accept-Language': 'zh-CN,zh;q=0.9,en;q=0.8',
        'Referer': 'https://www.douyin.com/'
      }
    };

    const req = https.request(options, (res) => {
      let data = '';

      res.on('data', (chunk) => {
        data += chunk;
      });

      res.on('end', () => {
        try {
          const jsonData = JSON.parse(data);
          resolve(jsonData);
        } catch (error) {
          reject(new Error(`JSON 解析失败: ${error.message}`));
        }
      });
    });

    req.on('error', (error) => {
      reject(error);
    });

    req.setTimeout(10000, () => {
      req.destroy();
      reject(new Error('请求超时'));
    });

    req.end();
  });
}

function formatHotBoard(data, limit = 50) {
  if (!data || !data.data || !data.data.word_list) {
    return [];
  }

  return data.data.word_list.slice(0, limit).map((item, index) => ({
    rank: index + 1,
    title: item.word || '无标题',
    popularity: item.hot_value || 0,
    link: item.url || `https://www.douyin.com/search/${encodeURIComponent(item.word || '')}`,
    label: item.label || null,
    type: item.type || '未知'
  }));
}

function printHotBoard(hotList) {
  console.log('🔥 抖音热榜 TOP ' + hotList.length);
  console.log('=' .repeat(70));
  console.log();

  hotList.forEach((item) => {
    console.log(`${item.rank.toString().padStart(2, ' ')}. ${item.title}`);
    console.log(`    🔥 热度: ${item.popularity.toLocaleString()}`);
    if (item.label) {
      console.log(`    🏷️  标签: ${item.label}`);
    }
    console.log(`    🔗 链接: ${item.link}`);
    console.log();
  });
}

// 主函数
async function main() {
  const args = process.argv.slice(2);
  const command = args[0] || 'hot';
  const limit = parseInt(args[1]) || 50;

  if (command === 'hot') {
    try {
      console.log('正在获取抖音热榜...\n');
      const data = await fetchDouyinHotBoard();
      const hotList = formatHotBoard(data, limit);

      if (hotList.length === 0) {
        console.log('❌ 未获取到热榜数据');
        process.exit(1);
      }

      printHotBoard(hotList);
    } catch (error) {
      console.error(`❌ 获取热榜失败: ${error.message}`);
      process.exit(1);
    }
  } else {
    console.log('用法:');
    console.log('  node scripts/douyin.js hot [数量]');
    console.log('');
    console.log('示例:');
    console.log('  node scripts/douyin.js hot      # 获取热榜（默认50条）');
    console.log('  node scripts/douyin.js hot 20   # 获取热榜前20条');
    process.exit(1);
  }
}

main();
