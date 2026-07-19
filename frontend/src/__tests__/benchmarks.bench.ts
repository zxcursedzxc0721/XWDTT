import { bench, describe } from 'vitest';
import { parseWdttUrl } from '../lib/utils/wdttLink';
import { renderMarkdown } from '../lib/utils/markdown';

// ═══════════════════════════════════════════════════
// parseWdttUrl
// ═══════════════════════════════════════════════════

const qwdttFull = 'qwdtt://config?name=MyServer&peer=1.2.3.4:5555&pass=secret&hashes=h1,h2,h3,h4&workers=18&port=9000';
const qwdttMinimal = 'qwdtt://config?peer=1.2.3.4:5555&pass=secret';
const legacyFull = 'wdtt://1.2.3.4:5555:9:10:password:hash1,hash2,hash3,hash4#MyServer';
const legacyMinimal = 'wdtt://1.2.3.4:5555:9:10:password';
const invalidUrl = 'http://example.com';
const longHashes = 'qwdtt://config?peer=1.2.3.4:5555&pass=secret&hashes=' + Array.from({ length: 20 }, (_, i) => `hash${i}`).join(',');

describe('parseWdttUrl', () => {
  bench('qwdtt full format', () => {
    parseWdttUrl(qwdttFull);
  });

  bench('qwdtt minimal', () => {
    parseWdttUrl(qwdttMinimal);
  });

  bench('legacy full format', () => {
    parseWdttUrl(legacyFull);
  });

  bench('legacy minimal', () => {
    parseWdttUrl(legacyMinimal);
  });

  bench('invalid URL (early return)', () => {
    parseWdttUrl(invalidUrl);
  });

  bench('qwdtt with 20 hashes', () => {
    parseWdttUrl(longHashes);
  });
});

// ═══════════════════════════════════════════════════
// renderMarkdown
// ═══════════════════════════════════════════════════

const markdownSimple = '# Header\n**Bold text** and `code`\n- item 1\n- item 2';
const markdownLinks = Array.from({ length: 10 }, (_, i) => `- [Link ${i}](https://example.com/page${i})`).join('\n');
const markdownHtml = '<script>alert(1)</script>\n<img onerror="alert(1)" src=x>\nNormal text with <b>tags</b>';
const markdownLong = Array.from({ length: 50 }, (_, i) => `## Section ${i}\n\n**Content ${i}** with \`code\` and [link](https://example.com/${i})\n\n- item ${i * 3}\n- item ${i * 3 + 1}\n- item ${i * 3 + 2}`).join('\n\n');

describe('renderMarkdown', () => {
  bench('simple markdown', () => {
    renderMarkdown(markdownSimple);
  });

  bench('10 links', () => {
    renderMarkdown(markdownLinks);
  });

  bench('HTML injection attempt (escaped)', () => {
    renderMarkdown(markdownHtml);
  });

  bench('long changelog (50 sections)', () => {
    renderMarkdown(markdownLong);
  });

  bench('empty string', () => {
    renderMarkdown('');
  });
});
