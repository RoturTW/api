package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func statusWSHandler(c *gin.Context) {
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	conn := &Conn{
		send:     make(chan []byte, 64),
		rooms:    make(map[string]struct{}),
		presence: PresenceOnline,
	}
	go conn.writePump(ws)
	conn.readPump(ws)
}

func (c *Conn) readPump(ws *websocket.Conn) {
	defer func() {
		hub.unregister(c)
		ws.Close()
	}()
	ws.SetReadLimit(4096)
	ws.SetReadDeadline(time.Now().Add(120 * time.Second))
	ws.SetPongHandler(func(string) error {
		ws.SetReadDeadline(time.Now().Add(120 * time.Second))
		return nil
	})
	for {
		_, raw, err := ws.ReadMessage()
		if err != nil {
			return
		}
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			c.sendError("invalid json")
			continue
		}
		cmdBytes, ok := msg["cmd"]
		if !ok {
			c.sendError("missing cmd")
			continue
		}
		var cmd string
		if err := json.Unmarshal(cmdBytes, &cmd); err != nil {
			c.sendError("invalid cmd")
			continue
		}
		if c.userId == "" && cmd != "auth" {
			c.sendError("not authenticated")
			continue
		}
		switch cmd {
		case "auth":
			c.handleAuth(msg)
		case "join":
			c.handleJoin(msg)
		case "leave":
			c.handleLeave(msg)
		case "rooms":
			c.handleRooms()
		case "set_status":
			c.handleSetStatus(msg)
		case "add_activity":
			c.handleAddActivity(msg)
		case "remove_activity":
			c.handleRemoveActivity(msg)
		case "room_state":
			c.handleRoomState(msg)
		default:
			c.sendError("unknown command")
		}
	}
}

func (c *Conn) writePump(ws *websocket.Conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		ws.Close()
	}()
	for {
		select {
		case data, ok := <-c.send:
			ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				ws.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Conn) sendMsg(payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	select {
	case c.send <- data:
	default:
	}
}

func (c *Conn) sendError(message string) {
	c.sendMsg(map[string]any{"cmd": "error", "message": message})
}

func (c *Conn) handleAuth(msg map[string]json.RawMessage) {
	if c.userId != "" {
		c.sendError("already authenticated")
		return
	}
	var key string
	if err := json.Unmarshal(msg["key"], &key); err != nil || key == "" {
		c.sendError("key required")
		return
	}
	user := authenticateWithKey(key)
	if user == nil {
		c.sendError("invalid key")
		return
	}
	c.userId = user.GetId()
	c.username = user.GetUsername()
	hub.register(c)

	hub.Lock()
	us := hub.userStatus[c.userId]
	if us == nil {
		presenceStr := "online"
		statusStr := ""
		if raw := user.Get("sys.status"); raw != nil {
			if m, ok := raw.(map[string]any); ok {
				presenceStr = strings.ToLower(getStringOrDefault(m["presence"], "online"))
				statusStr = getStringOrDefault(m["status"], "")
			}
		}
		us = &UserStatus{
			Presence:   Presence(presenceStr),
			Status:     statusStr,
			Activities: make(map[string]Activity),
		}
		hub.userStatus[c.userId] = us
	}
	c.presence = us.Presence
	c.lastPresenceSet = time.Now()
	hub.Unlock()

	userObj := userToNet(*user)
	userObj["sys.status"] = map[string]any{
		"presence": string(us.Presence),
		"status":   us.Status,
	}

	c.sendMsg(map[string]any{
		"cmd":      "ready",
		"user_id":  string(c.userId),
		"username": string(c.username),
		"user":     userObj,
	})
}

func (c *Conn) handleJoin(msg map[string]json.RawMessage) {
	var rooms []string
	if err := json.Unmarshal(msg["rooms"], &rooms); err != nil {
		var single string
		if err := json.Unmarshal(msg["rooms"], &single); err != nil {
			c.sendError("rooms required")
			return
		}
		rooms = []string{single}
	}
	if len(rooms) == 0 {
		c.sendError("rooms required")
		return
	}
	for _, name := range rooms {
		if len(name) > maxRoomNameLen || !roomNameRe.MatchString(name) {
			c.sendError("invalid room name: " + name)
			return
		}
	}
	hub.Lock()
	if len(c.rooms)+len(rooms) > maxRoomsPerConn {
		hub.Unlock()
		c.sendError("room limit reached")
		return
	}
	for _, name := range rooms {
		if _, exists := c.rooms[name]; exists {
			hub.Unlock()
			c.sendError("already in room: " + name)
			return
		}
	}
	for _, name := range rooms {
		c.rooms[name] = struct{}{}
		r := hub.getOrMakeRoom(name)
		r.members[c.userId] = struct{}{}
	}
	state := hub.mergedStateLocked(c.userId)
	var snapshots map[string][]RoomMember
	snapshots = make(map[string][]RoomMember, len(rooms))
	for _, name := range rooms {
		snapshots[name] = roomSnapshotLocked(hub, name)
	}
	hub.Unlock()
	for _, name := range rooms {
		c.sendMsg(map[string]any{"cmd": "join_ok", "room": name})
		c.sendMsg(map[string]any{
			"cmd":     "room_state",
			"room":    name,
			"members": snapshots[name],
		})
		if state.Presence.visible() {
			hub.broadcast(name, "member_join", map[string]any{
				"room":       name,
				"user_id":    string(c.userId),
				"username":   string(c.username),
				"status":     state.Status,
				"presence":   string(state.Presence),
				"activities": state.Activities,
			}, c.userId)
		}
	}
}

func (c *Conn) handleLeave(msg map[string]json.RawMessage) {
	var rooms []string
	if err := json.Unmarshal(msg["rooms"], &rooms); err != nil {
		var single string
		if err := json.Unmarshal(msg["rooms"], &single); err != nil {
			c.sendError("rooms required")
			return
		}
		rooms = []string{single}
	}
	if len(rooms) == 0 {
		c.sendError("rooms required")
		return
	}
	hub.Lock()
	for _, name := range rooms {
		if _, exists := c.rooms[name]; !exists {
			hub.Unlock()
			c.sendError("not in room: " + name)
			return
		}
	}
	for _, name := range rooms {
		delete(c.rooms, name)
	}
	for _, name := range rooms {
		r, ok := hub.rooms[name]
		if !ok {
			continue
		}
		stillInRoom := false
		for _, cc := range hub.userConns[c.userId] {
			if cc == c {
				continue
			}
			if _, inRoom := cc.rooms[name]; inRoom {
				stillInRoom = true
				break
			}
		}
		if stillInRoom {
			state := hub.mergedStateLocked(c.userId)
			hub.broadcastLocked(name, "status_update", map[string]any{
				"room":     name,
				"user_id":  string(c.userId),
				"username": string(c.username),
				"status":   state.Status,
				"presence": string(state.Presence),
			}, c.userId)
		} else {
			delete(r.members, c.userId)
			hub.broadcastLocked(name, "member_leave", map[string]any{
				"room":    name,
				"user_id": string(c.userId),
			}, "")
			if len(r.members) == 0 {
				delete(hub.rooms, name)
			}
		}
	}
	hub.Unlock()
	for _, name := range rooms {
		c.sendMsg(map[string]any{"cmd": "leave_ok", "room": name})
	}
}

func (c *Conn) handleRooms() {
	hub.Lock()
	rooms := make([]string, 0, len(c.rooms))
	for name := range c.rooms {
		rooms = append(rooms, name)
	}
	hub.Unlock()
	c.sendMsg(map[string]any{"cmd": "rooms", "rooms": rooms})
}

func (c *Conn) handleSetStatus(msg map[string]json.RawMessage) {
	var status string
	var statusSet bool
	if b, ok := msg["status"]; ok {
		statusSet = true
		if err := json.Unmarshal(b, &status); err != nil {
			c.sendError("invalid status")
			return
		}
		if len(status) > maxStatusLen {
			c.sendError("status too long")
			return
		}
	}

	var p string
	var presenceSet bool
	if b, ok := msg["presence"]; ok {
		presenceSet = true
		if err := json.Unmarshal(b, &p); err != nil {
			c.sendError("invalid presence")
			return
		}
		p = strings.ToLower(p)
		switch Presence(p) {
		case PresenceOnline, PresenceIdle, PresenceDND, PresenceInvisible:
		default:
			c.sendError("invalid presence value")
			return
		}
	}

	if !statusSet && !presenceSet {
		c.sendError("status or presence required")
		return
	}

	hub.Lock()
	us := hub.userStatus[c.userId]
	if us == nil {
		us = &UserStatus{Presence: PresenceOnline, Activities: make(map[string]Activity)}
		hub.userStatus[c.userId] = us
	}

	if presenceSet && c.presence == Presence(p) && statusSet && us.Status == status {
		hub.Unlock()
		return
	}
	if presenceSet && !statusSet && c.presence == Presence(p) {
		hub.Unlock()
		return
	}
	if statusSet && !presenceSet && us.Status == status {
		hub.Unlock()
		return
	}

	oldMerged := hub.mergedPresenceLocked(c.userId)

	if presenceSet {
		c.presence = Presence(p)
		c.lastPresenceSet = time.Now()
		us.Presence = Presence(p)
	}
	if statusSet {
		us.Status = status
	}

	newMerged := hub.mergedPresenceLocked(c.userId)
	roomNames := hub.allRoomsForUserLocked(c.userId)
	hub.Unlock()

	user := getUserById(c.userId)
	if user != nil {
		user.Set("sys.status", map[string]any{
			"presence": string(us.Presence),
			"status":   us.Status,
		})
		go saveUsers()
	}

	wasVisible := oldMerged.visible()
	nowVisible := newMerged.visible()

	if wasVisible && !nowVisible {
		for _, name := range roomNames {
			hub.broadcast(name, "member_leave", map[string]any{
				"room":    name,
				"user_id": string(c.userId),
			}, "")
		}
	} else if !wasVisible && nowVisible {
		hub.Lock()
		state := hub.mergedStateLocked(c.userId)
		hub.Unlock()
		for _, name := range roomNames {
			hub.broadcast(name, "member_join", map[string]any{
				"room":       name,
				"user_id":    string(c.userId),
				"username":   string(c.username),
				"status":     state.Status,
				"presence":   string(state.Presence),
				"activities": state.Activities,
			}, "")
		}
	} else {
		hub.broadcastStatusToAllRooms(c.userId, roomNames)
	}
}

func (c *Conn) handleAddActivity(msg map[string]json.RawMessage) {
	var act Activity
	raw, ok := msg["id"]
	if !ok {
		c.sendError("id required")
		return
	}
	if err := json.Unmarshal(raw, &act.ID); err != nil || act.ID == "" {
		c.sendError("invalid id")
		return
	}
	if b, ok := msg["title"]; ok {
		json.Unmarshal(b, &act.Title)
	}
	if b, ok := msg["application"]; ok {
		var app ActivityApplication
		json.Unmarshal(b, &app)
		act.Application = &app
	}
	if b, ok := msg["image"]; ok {
		json.Unmarshal(b, &act.Image)
	}
	if b, ok := msg["url"]; ok {
		json.Unmarshal(b, &act.URL)
	}
	if b, ok := msg["status"]; ok {
		json.Unmarshal(b, &act.Status)
	}
	if b, ok := msg["start_time"]; ok {
		json.Unmarshal(b, &act.StartTime)
	}
	if b, ok := msg["media"]; ok {
		var media ActivityMedia
		json.Unmarshal(b, &media)
		act.Media = &media
	}

	hub.Lock()
	us := hub.userStatus[c.userId]
	if us == nil {
		us = &UserStatus{Presence: PresenceOnline, Activities: make(map[string]Activity)}
		hub.userStatus[c.userId] = us
	}
	if len(us.Activities) >= maxActivitiesPerConn {
		hub.Unlock()
		c.sendError("activity limit reached")
		return
	}
	us.Activities[act.ID] = act

	roomNames := hub.allRoomsForUserLocked(c.userId)
	hub.Unlock()
	hub.broadcastStatusToAllRooms(c.userId, roomNames)
}

func (c *Conn) handleRemoveActivity(msg map[string]json.RawMessage) {
	var id string
	if err := json.Unmarshal(msg["id"], &id); err != nil || id == "" {
		c.sendError("id required")
		return
	}
	hub.Lock()
	us := hub.userStatus[c.userId]
	if us == nil {
		hub.Unlock()
		c.sendError("activity not found")
		return
	}
	if _, exists := us.Activities[id]; !exists {
		hub.Unlock()
		c.sendError("activity not found")
		return
	}
	delete(us.Activities, id)

	roomNames := hub.allRoomsForUserLocked(c.userId)
	hub.Unlock()
	hub.broadcastStatusToAllRooms(c.userId, roomNames)
}

func (c *Conn) handleRoomState(msg map[string]json.RawMessage) {
	var room string
	if err := json.Unmarshal(msg["room"], &room); err != nil || room == "" {
		c.sendError("room required")
		return
	}
	hub.Lock()
	if _, inRoom := c.rooms[room]; !inRoom {
		hub.Unlock()
		c.sendError("not in room: " + room)
		return
	}
	snapshot := roomSnapshotLocked(hub, room)
	hub.Unlock()
	c.sendMsg(map[string]any{
		"cmd":     "room_state",
		"room":    room,
		"members": snapshot,
	})
}

func (c *Conn) roomList() []string {
	rooms := make([]string, 0, len(c.rooms))
	for name := range c.rooms {
		rooms = append(rooms, name)
	}
	return rooms
}

func activityFromMap(id string, m map[string]any) Activity {
	act := Activity{ID: id}
	if v, ok := m["title"].(string); ok {
		act.Title = v
	}
	if v, ok := m["image"].(string); ok {
		act.Image = v
	}
	if v, ok := m["url"].(string); ok {
		act.URL = v
	}
	if app, ok := m["application"].(map[string]any); ok {
		a := &ActivityApplication{}
		if v, ok := app["name"].(string); ok {
			a.Name = v
		}
		if v, ok := app["url"].(string); ok {
			a.URL = v
		}
		act.Application = a
	}
	if media, ok := m["media"].(map[string]any); ok {
		md := &ActivityMedia{}
		if v, ok := media["title"].(string); ok {
			md.Title = v
		}
		if v, ok := media["artist"].(string); ok {
			md.Artist = v
		}
		if v, ok := media["album"].(string); ok {
			md.Album = v
		}
		if v, ok := media["start"].(float64); ok {
			md.Start = int64(v)
		}
		if v, ok := media["end"].(float64); ok {
			md.End = int64(v)
		}
		act.Media = md
	}
	return act
}

func statusGetHTTP(c *gin.Context) {
	name := c.Query("name")
	if name == "" {
		c.JSON(400, gin.H{"error": "name parameter missing"})
		return
	}
	uid := Username(name).ToLower().Id()
	if uid == "" {
		c.JSON(404, gin.H{"error": "user not found"})
		return
	}
	state := hub.getUserStatus(uid)
	if state == nil {
		c.JSON(404, gin.H{"error": "no status"})
		return
	}
	c.JSON(200, gin.H{
		"username":   name,
		"status":     state.Status,
		"presence":   string(state.Presence),
		"activities": state.Activities,
	})
}
