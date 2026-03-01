---
name: browser
description: How to use browser tool against this app's session auth. Use when taking screenshots, testing UI, or interacting with authenticated pages via the browser tool.
---

Simulate an authenticated user for browser tool use.

Insert a session:

```bash
sqlite3 /home/exedev/db.sqlite3 \
  "INSERT OR REPLACE INTO sessions (id, user_id, email, expires_at) VALUES ('browser-test-session', 'test-user', 'test@housecat.com', datetime('now', '+1 day'));"
```

Set a cookie in the browser:

```javascript
// Use browser eval action:
document.cookie = "session_id=browser-test-session; path=/";
```

Now navigate as usual.
