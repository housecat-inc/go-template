---
name: browser
description: How to use browser tool against this app's session auth. Use when taking screenshots, testing UI, or interacting with authenticated pages via the browser tool.
---

When running on exe.dev, visit any authenticated page first — the app auto-creates a session cookie from the exe.dev headers. After that, the browser tool can access authenticated pages using the cookie.

If the app is running locally without exe.dev or OAuth, insert a session manually:

```bash
sqlite3 db.sqlite3 \
  "INSERT OR REPLACE INTO sessions (id, user_id, email, expires_at) VALUES ('browser-test-session', 'test-user', 'test@housecat.com', datetime('now', '+1 day'));"
```

Then set the cookie in the browser:

```javascript
document.cookie = "session_id=browser-test-session; path=/";
```
