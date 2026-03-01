---
name: browser-auth
description: How to authenticate browser tool requests against this app's session auth. Use when taking screenshots, testing UI, or interacting with authenticated pages via the browser tool.
---

# Browser Authentication

## Problem

The browser tool accesses `http://localhost:8000` directly, bypassing the exe.dev proxy that normally injects `X-ExeDev-UserID` and `X-ExeDev-Email` headers. Authenticated routes redirect to `/` (the login page) without valid credentials.

## Solution: Create a session via sqlite3

Insert a session row directly into the database and set the cookie in the browser.

### Step 1: Insert a session

```bash
sqlite3 /home/exedev/db.sqlite3 \
  "INSERT OR REPLACE INTO sessions (id, user_id, email, expires_at) VALUES ('browser-test-session', 'test-user', 'noah@housecat.com', datetime('now', '+1 day'));"
```

### Step 2: Set the cookie in the browser

```javascript
// Use browser eval action:
document.cookie = "session_id=browser-test-session; path=/";
```

### Step 3: Navigate normally

```javascript
// Now authenticated pages work:
// browser navigate to http://localhost:8000/chats
```

## Alternative: Fetch with headers

For quick one-off checks, fetch the page with auth headers and replace the document. This loads the HTML but won't persist auth for subsequent navigations or asset loading.

```javascript
// Use browser eval action after navigating to http://localhost:8000:
fetch('/chats', {
  headers: {
    'X-ExeDev-UserID': 'test-user',
    'X-ExeDev-Email': 'noah@housecat.com'
  }
}).then(r => r.text()).then(html => {
  document.open();
  document.write(html);
  document.close();
});
```

This works for screenshots since CSS/JS assets are on public paths (`/assets/*`), but links and form submissions won't carry the headers.

## Auth flow reference

The `RequireAuth` middleware in `srv/auth.go` checks in order:

1. **Session cookie**: Looks for `session_id` cookie, validates against `sessions` table
2. **Proxy headers**: Falls back to `X-ExeDev-UserID` and `X-ExeDev-Email` headers
3. **Redirect**: If neither exists, redirects to `/`

The middleware sets three context values: `userID`, `userEmail`, `logoutURL`.

## Cleanup

The test session can be removed with:

```bash
sqlite3 /home/exedev/db.sqlite3 "DELETE FROM sessions WHERE id = 'browser-test-session';"
```

## Recommended approach

Prefer the **session cookie method** for multi-page testing and screenshots. Use the **fetch with headers method** for quick single-page checks.
