#!/usr/bin/env node
/**
 * 抖音热榜抓取脚本 - JSON输出版本
 * 获取抖音热搜榜数据并输出JSON
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

// 主函数
async function main() {
  const args = process.argv.slice(2);
  const limit = parseInt(args[0]) || 50;

  try {
    const data = await fetchDouyinHotBoard();
    const hotList = formatHotBoard(data, limit);

    if (hotList.length === 0) {
      console.log(JSON.stringify({ error: '未获取到热榜数据' }));
      process.exit(1);
    }

    // 输出JSON格式
    console.log(JSON.stringify(hotList));
  } catch (error) {
    console.log(JSON.stringify({ error: error.message }));
    process.exit(1);
  }
}

main();
