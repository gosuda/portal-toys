/**
 * Markdown-like code block renderer using highlight.js
 * Supports:
 * - Multi-line code blocks: ```language ... ```
 * - Inline code: `code`
 */

// Load highlight.js dynamically
(function() {
  // Add highlight.js CSS
  const link = document.createElement('link');
  link.rel = 'stylesheet';
  link.href = 'https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/styles/github-dark.min.css';
  document.head.appendChild(link);

  // Add highlight.js script
  const script = document.createElement('script');
  script.src = 'https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/highlight.min.js';
  script.onload = function() {
    console.log('[markdown.js] highlight.js loaded');
  };
  document.head.appendChild(script);
})();

// Custom styles for code blocks
(function() {
  const style = document.createElement('style');
  style.textContent = `
    /* Multi-line code block */
    .code-block-wrapper {
      display: block;
      margin: 8px 0;
      border-radius: 6px;
      overflow: hidden;
      background: #161b22;
      border: 1px solid #30363d;
    }
    .code-block-header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding: 6px 12px;
      background: #21262d;
      border-bottom: 1px solid #30363d;
      font-size: 12px;
      color: #8b949e;
    }
    .code-block-lang {
      font-weight: 500;
      text-transform: uppercase;
    }
    .code-block-copy {
      background: transparent;
      border: 1px solid #30363d;
      color: #8b949e;
      padding: 2px 8px;
      border-radius: 4px;
      cursor: pointer;
      font-size: 11px;
      transition: all 0.2s;
    }
    .code-block-copy:hover {
      background: #30363d;
      color: #c9d1d9;
    }
    .code-block-copy.copied {
      background: #238636;
      border-color: #238636;
      color: #fff;
    }
    .code-block-content {
      margin: 0;
      padding: 12px;
      overflow-x: auto;
      font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
      font-size: 13px;
      line-height: 1.45;
    }
    .code-block-content code {
      background: transparent !important;
      padding: 0 !important;
      border-radius: 0 !important;
      font-size: inherit;
      color: #c9d1d9;
    }

    /* Inline code */
    .inline-code {
      background: #161b22;
      border: 1px solid #30363d;
      border-radius: 4px;
      padding: 2px 6px;
      font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
      font-size: 0.9em;
      color: #c9d1d9;
    }
  `;
  document.head.appendChild(style);
})();

/**
 * Render markdown-like code blocks in text
 * @param {string} text - Raw text that may contain code blocks
 * @returns {string} - HTML string with rendered code blocks
 */
function renderMarkdown(text) {
  if (!text) return text;

  let result = text;

  // Process multi-line code blocks first: ```lang\ncode\n```
  // Match code blocks with optional language specifier
  const codeBlockRegex = /```(\w*)\n?([\s\S]*?)```/g;

  result = result.replace(codeBlockRegex, function(match, lang, code, offset, fullString) {
    const language = lang || 'plaintext';
    const escapedCode = escapeHtmlForCode(code.trim());
    const uniqueId = 'code-' + Math.random().toString(36).substr(2, 9);

    // Check if there's text before the code block (not just whitespace/newline)
    const beforeText = fullString.substring(0, offset);
    const hasTextBefore = beforeText.length > 0 && !/[\n\r]$/.test(beforeText) && beforeText.trim().length > 0;

    // Check if there's text after the code block
    const afterOffset = offset + match.length;
    const afterText = fullString.substring(afterOffset);
    const hasTextAfter = afterText.length > 0 && !/^[\n\r]/.test(afterText) && afterText.trim().length > 0;

    // Build the code block HTML
    let html = '';

    // Add line break before if there's text immediately before
    if (hasTextBefore) {
      html += '<br>';
    }

    html += '<div class="code-block-wrapper">' +
      '<div class="code-block-header">' +
        '<span class="code-block-lang">' + language + '</span>' +
        '<button class="code-block-copy" data-code-id="' + uniqueId + '" onclick="copyCodeBlock(this, \'' + uniqueId + '\')">Copy</button>' +
      '</div>' +
      '<pre class="code-block-content"><code id="' + uniqueId + '" class="language-' + language + '">' + escapedCode + '</code></pre>' +
    '</div>';

    // Add line break after if there's text immediately after
    if (hasTextAfter) {
      html += '<br>';
    }

    return html;
  });

  // Process inline code: `code` (but not inside code blocks)
  // Only match single backticks that don't span multiple lines
  const inlineCodeRegex = /`([^`\n]+)`/g;
  result = result.replace(inlineCodeRegex, function(match, code) {
    const escapedCode = escapeHtmlForCode(code);
    return '<span class="inline-code">' + escapedCode + '</span>';
  });

  return result;
}

/**
 * Escape HTML special characters for code display
 */
function escapeHtmlForCode(str) {
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');
}

/**
 * Copy code block content to clipboard
 */
function copyCodeBlock(button, codeId) {
  const codeElement = document.getElementById(codeId);
  if (!codeElement) return;

  const text = codeElement.textContent;
  navigator.clipboard.writeText(text).then(function() {
    button.textContent = 'Copied!';
    button.classList.add('copied');
    setTimeout(function() {
      button.textContent = 'Copy';
      button.classList.remove('copied');
    }, 2000);
  }).catch(function(err) {
    console.error('Failed to copy:', err);
  });
}

/**
 * Apply syntax highlighting to all code blocks on the page
 * Call this after adding new content to the DOM
 */
function highlightAllCodeBlocks() {
  if (typeof hljs === 'undefined') {
    // hljs not loaded yet, retry after a short delay
    setTimeout(highlightAllCodeBlocks, 100);
    return;
  }

  document.querySelectorAll('.code-block-content code:not(.hljs)').forEach(function(block) {
    hljs.highlightElement(block);
  });
}

// Export for use in view.go
window.renderMarkdown = renderMarkdown;
window.highlightAllCodeBlocks = highlightAllCodeBlocks;
window.copyCodeBlock = copyCodeBlock;
