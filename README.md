# api.rotur.dev

this is the centralised backend for all of rotur.

Environment Variables (loaded from root ../.env with optional local overrides):

```txt
USERS_FILE_PATH             - where in the file system the users.json is
LOCAL_POSTS_PATH            - where to store claw posts, eg: posts.json
FOLLOWERS_FILE_PATH         - where to store the follower data for claw, eg: ./clawusers.json
ITEMS_FILE_PATH             - where to store item data for rotur, eg: ./items.json
KEYS_FILE_PATH              - where to store key data for rotur, eg: ./keys.json
EVENTS_HISTORY_PATH         - where to put account events, eg: ./events_history.json
DAILY_CLAIMS_FILE_PATH      - where to store daily claim data, eg: ./rotur_daily.json
SYSTEMS_FILE_PATH           - where in the file system info about rotur systems is, eg: ./systems.json
WEBSOCKET_SERVER_URL        - claw events go here
EVENT_SERVER_URL            - websocket events go here
SUBSCRIPTION_CHECK_INTERVAL - 3600
BANNED_WORDS_URL            - a list of banned words for claw posts and other user data, eg: "http://www.bannedwordlist.com/lists/swearWords.txt"
DISCORD_WEBHOOK_URL         - where to log claw posts to
KEY_OWNERSHIP_CACHE_TTL     - 600
ADMIN_TOKEN                 - a token used for authenticating locally between other rotur apis
```

## HTTP API Endpoints

All endpoints are served on port 5602 (example: `http://localhost:5602`). Unless otherwise noted, query parameters are passed via `?param=value`. JSON bodies are used for POST/PATCH where described.

### Posts
- `GET /post` Create a post (query: `auth`, `content`, optional `attachment`, `os`, `profile_only=1`)
- `GET /reply` Reply to a post
- `GET /delete` Delete a post
- `GET /rate` Rate (like?) a post
- `GET /repost` Repost a post
- `GET /pin_post` Pin a post to profile
- `GET /unpin_post` Unpin a post
- `GET /search_posts` Search posts (query-based)
- `GET /top_posts` Get top liked posts within time/limit

### Feeds
- `GET /feed` Public feed (params: `limit`, `offset`)
- `GET /following_feed` Feed of followed users

### Following / Social Graph
- `GET /follow` Follow a user
- `GET /unfollow` Unfollow a user
- `GET /followers` List followers of a user
- `GET /following` List following for a user
- `GET /notifications` Get notifications for authenticated user

### Profiles & Users
- `GET /profile` Get profile by username
- `GET /get_user` Get user by auth key or username (legacy/new alias: `/get_user_new`)
- `POST /create_user` Register a new user (JSON body)
- `PATCH /users` Update a user key/value (JSON body: `auth`, `key`, `value`)
- `DELETE /users/:username` Delete (admin?) user by username
- `DELETE /users` Delete user using auth key (JSON?)
- `POST /me/update` Update current user (alias of update)
- `DELETE /me/delete` Delete current user (key-based)
- `GET /me` Get current user (auth)
- `POST /me/refresh_token` Refresh an auth token
- `POST /me/transfer` Transfer credits
- `POST /me/gamble` Gamble credits

### Search
- `GET /search_users` Search users

### Systems
- `GET /systems` List systems
- `GET /reload_systems` Reload system definitions (admin)

### Validators
- `GET /generate_validator` Generate validator token
- `GET /validate` Validate a token

### Status / Health
- `GET /status` General status (startup uptime etc.)
- `GET /status/update` Set status (auth)
- `GET /status/clear` Clear status
- `GET /status/get` Get status for user

### Economy / Stats
- `GET /stats/economy` Economy stats
- `GET /stats/users` User stats
- `GET /stats/rich` Rich list
- `GET /stats/aura` Aura stats
- `GET /stats/systems` System stats
- `GET /stats/followers` Followers stats
- `GET /supporters` Supporters list
- `GET /claim_daily` Claim daily reward

### Items
- `GET /items/transfer/:name` Transfer ownership
- `GET /items/buy/:name` Buy item
- `GET /items/stop_selling/:name` Stop selling
- `GET /items/set_price/:name` Set price (seller/admin?)
- `GET /items/create` Create item
- `GET /items/get/:name` Get item info
- `GET /items/delete/:name` Delete item
- `GET /items/list/:username` List a user's items
- `GET /items/update/:name` Update item meta
- `GET /items/sell/:name` Put item for sale
- `GET /items/selling` List currently selling items
- `GET /items/admin_add/:id` Admin add user to item

### Keys
- `GET /keys/create` Create key
- `GET /keys/mine` List my keys
- `GET /keys/check/:username` Check if user owns key
- `GET /keys/revoke/:id` Revoke key
- `GET /keys/delete/:id` Delete key
- `GET /keys/update/:id` Update key metadata
- `GET /keys/name/:id` Set key name
- `GET /keys/get/:id` Get key info
- `GET /keys/admin_add/:id` Admin add user to key
- `GET /keys/admin_remove/:id` Admin remove user from key
- `GET /keys/buy/:id` Buy key
- `GET /keys/cancel/:id` Cancel key purchase
- `GET /keys/debug_subscriptions` Debug subscription status

### Friends
- `POST /friends/request/:username` Send friend request
- `POST /friends/accept/:username` Accept request
- `POST /friends/reject/:username` Reject request
- `POST /friends/remove/:username` Remove friend
- `GET /friends` List friends & pending

### Marriage
- `POST /marriage/propose/:username` Propose marriage
- `POST /marriage/accept` Accept marriage proposal
- `POST /marriage/reject` Reject marriage proposal
- `POST /marriage/cancel` Cancel marriage proposal
- `POST /marriage/divorce` Divorce spouse
- `GET /marriage/status` Get marriage status

### Linking External Accounts
- `GET /link/code` Request a link code
- `POST /link/code` Link code to account
- `GET /link/status` Get link status
- `GET /link/user` Get linked user info

### DevFund / Escrow
- `POST /devfund/escrow_transfer` Start escrow transfer
- `POST /devfund/escrow_release` Release escrow

### Admin Ops
- `GET /admin/get_user_by` Get user by field
- `POST /admin/update_user` Admin update user (typed operations)
- `POST /admin/delete_user` Admin delete user

### Terms of Service
- `POST /accept_tos` Accept terms of service
