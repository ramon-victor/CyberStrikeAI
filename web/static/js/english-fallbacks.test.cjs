const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');

test('HTML fallbacks remain English before language resources load', () => {
 for (const file of ['web/templates/index.html', 'web/templates/api-docs.html']) {
  const source = fs.readFileSync(file, 'utf8')
   .replace(/<!--[\s\S]*?-->/g, '')
   .replace(/<script\b[\s\S]*?<\/script>/gi, '')
   .replace(/<style\b[\s\S]*?<\/style>/gi, '');
  for (const [, text] of source.matchAll(/>([^<>]+)</g)) {
   if (text.trim() === '中文') continue; // Native name in the optional language picker.
   assert.doesNotMatch(text, /\p{Script=Han}/u, `${file}: untranslated visible text`);
  }
  for (const [, value] of source.matchAll(/\b(?:title|placeholder|aria-label|alt|data-title)="([^"]*)"/g)) {
   assert.doesNotMatch(value, /\p{Script=Han}/u, `${file}: untranslated accessible text`);
  }
 }
});
