// Type definitions for app.js

// User type for roster
interface OnlineUser {
  name: string;
  uid?: string;
}

// Message type
interface ChatMessage {
  ts: string;
  user?: string;
  text?: string;
  event?: 'roster' | 'rename' | 'joined' | 'left' | 'token';
  token?: string;
  users?: string[];
  userList?: OnlineUser[];
  isAdmin?: boolean;
}

// Pending message for bubble display
interface PendingMessage {
  username: string;
  text: string;
}

// Functions from markdown.js
declare function renderMarkdown(text: string): string;
declare function highlightAllCodeBlocks(): void;

// Extend Window interface for debug function
interface Window {
  debugWS: () => void;
}
