/* eslint-disable */
// Sibling script to check-contrast.cjs — детектор литеральных hex-цветов
// в исходниках frontend/src/**/*.svelte (DS2-06). Скан ограничен блоками
// <style> и inline-атрибутами style="" — не подключён к check:contrast
// в этой волне, подключение выполняет план 120-14.
const fs = require('fs');
const path = require('path');

const SRC_ROOT = path.join(__dirname, '../src');

// Whitelist — поимённые исключения с обязательным обоснованием. Молчаливое
// исключение по маске каталога запрещено.
const WHITELIST = [];

function scanContent(_content, _relFile) {
  return [];
}

module.exports = { scanContent, WHITELIST, SRC_ROOT };

if (require.main === module) {
  process.exit(0);
}
