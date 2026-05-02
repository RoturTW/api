package main

import (
	"encoding/json"
	"log"
	"maps"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	maxRoomsPerConn      = 200
	maxActivitiesPerConn = 5
	maxStatusLen         = 128
	maxRoomNameLen       = 64
)

type Presence string

const (
	PresenceOnline    Presence = "online"
	PresenceIdle      Presence = "idle"
	PresenceDND       Presence = "dnd"
	PresenceInvisible Presence = "invisible"
)

func (p Presence) visible() bool {
	return p != PresenceInvisible
}

var roomNameRe = regexp.MustCompile(`^[a-zA-Z0-9_\-:.]+$`)

type ActivityMedia struct {
	Title  string `json:"title"`
	Artist string `json:"artist,omitempty"`
	Album  string `json:"album,omitempty"`
	Start  int64  `json:"start"`
	End    int64  `json:"end"`
}

type ActivityApplication struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

type Activity struct {
	ID          string               `json:"id"`
	Title       string               `json:"title"`
	Application *ActivityApplication `json:"application,omitempty"`
	Image       string               `json:"image,omitempty"`
	URL         string               `json:"url,omitempty"`
	Status      string               `json:"status,omitempty"`
	StartTime   int64                `json:"start_time,omitempty"`
	Media       *ActivityMedia       `json:"media,omitempty"`
}

type RoomMember struct {
	UserID     UserId     `json:"user_id"`
	Username   Username   `json:"username"`
	Status     string     `json:"status"`
	Presence   Presence   `json:"presence"`
	Activities []Activity `json:"activities"`
}

type UserStatus struct {
	Status     string              `json:"status"`
	Presence   Presence            `json:"presence"`
	Activities map[string]Activity `json:"activities"`
}

type Room struct {
	name    string
	members map[UserId]struct{}
}

type Conn struct {
	send            chan []byte
	userId          UserId
	username        Username
	rooms           map[string]struct{}
	presence        Presence
	lastPresenceSet time.Time
	activities      map[string]struct{}
}

type Hub struct {
	sync.Mutex
	conns      map[*Conn]struct{}
	rooms      map[string]*Room
	userConns  map[UserId][]*Conn
	userStatus map[UserId]*UserStatus
}

var hub = &Hub{
	conns:      make(map[*Conn]struct{}),
	rooms:      make(map[string]*Room),
	userConns:  make(map[UserId][]*Conn),
	userStatus: make(map[UserId]*UserStatus),
}

func (h *Hub) register(c *Conn) {
	h.Lock()
	h.conns[c] = struct{}{}
	h.userConns[c.userId] = append(h.userConns[c.userId], c)
	if _, ok := h.userStatus[c.userId]; !ok {
		h.userStatus[c.userId] = &UserStatus{
			Presence:   PresenceOnline,
			Activities: make(map[string]Activity),
		}
	}
	h.Unlock()
}

func (h *Hub) removeConnActivitiesLocked(c *Conn) {
	if len(c.activities) == 0 {
		return
	}

	us := h.userStatus[c.userId]
	if us == nil {
		c.activities = nil
		return
	}

	if us.Activities == nil {
		us.Activities = make(map[string]Activity)
	}

	for id := range c.activities {
		delete(us.Activities, id)
	}
	c.activities = nil
}

func (h *Hub) unregister(c *Conn) {
	h.Lock()
	delete(h.conns, c)
	h.removeConnActivitiesLocked(c)

	conns := h.userConns[c.userId]
	for i, cc := range conns {
		if cc == c {
			h.userConns[c.userId] = append(conns[:i], conns[i+1:]...)
			break
		}
	}
	if len(h.userConns[c.userId]) == 0 {
		us := h.userStatus[c.userId]
		if us != nil {
			h.persistStatusLocked(c.userId, us)
		}
		delete(h.userConns, c.userId)
		delete(h.userStatus, c.userId)
	}

	leftRooms := make([]string, 0, len(c.rooms))
	for name := range c.rooms {
		leftRooms = append(leftRooms, name)
	}
	for _, roomName := range leftRooms {
		r, ok := h.rooms[roomName]
		if !ok {
			continue
		}
		stillInRoom := false
		for _, cc := range h.userConns[c.userId] {
			if _, inRoom := cc.rooms[roomName]; inRoom {
				stillInRoom = true
				break
			}
		}
		if stillInRoom {
			state := h.mergedStateLocked(c.userId)
			if state.Presence.visible() {
				h.broadcastLocked(roomName, "status_update", map[string]any{
					"room":     roomName,
					"user_id":  string(c.userId),
					"username": string(c.username),
					"status":   state.Status,
					"presence": string(state.Presence),
				}, c.userId)
			}
		} else {
			delete(r.members, c.userId)
			h.broadcastLocked(roomName, "member_leave", map[string]any{
				"room":    roomName,
				"user_id": string(c.userId),
			}, "")
			if len(r.members) == 0 {
				delete(h.rooms, roomName)
			}
		}
	}
	c.rooms = nil
	if len(h.userConns[c.userId]) > 0 {
		roomNames := h.allRoomsForUserLocked(c.userId)
		h.Unlock()
		h.broadcastStatusToAllRooms(c.userId, roomNames)
	} else {
		h.Unlock()
	}
}

func (h *Hub) getOrMakeRoom(name string) *Room {
	r, ok := h.rooms[name]
	if !ok {
		r = &Room{name: name, members: make(map[UserId]struct{})}
		h.rooms[name] = r
	}
	return r
}

func (h *Hub) broadcast(roomName, cmd string, payload map[string]any, excludeUserId UserId) {
	h.Lock()
	defer h.Unlock()
	h.broadcastLocked(roomName, cmd, payload, excludeUserId)
}

func (h *Hub) broadcastLocked(roomName, cmd string, payload map[string]any, excludeUserId UserId) {
	r, ok := h.rooms[roomName]
	if !ok {
		return
	}
	wrapper := map[string]any{"cmd": cmd}
	maps.Copy(wrapper, payload)
	data, err := json.Marshal(wrapper)
	if err != nil {
		return
	}
	seen := make(map[*Conn]struct{})
	for uid := range r.members {
		if uid == excludeUserId {
			continue
		}
		for _, c := range h.userConns[uid] {
			if _, inRoom := c.rooms[roomName]; !inRoom {
				continue
			}
			if _, ok := seen[c]; ok {
				continue
			}
			seen[c] = struct{}{}
			select {
			case c.send <- data:
			default:
			}
		}
	}
}

func (h *Hub) allRoomsForUserLocked(uid UserId) []string {
	seen := make(map[string]struct{})
	rooms := make([]string, 0)
	for _, c := range h.userConns[uid] {
		for name := range c.rooms {
			if _, ok := seen[name]; !ok {
				seen[name] = struct{}{}
				rooms = append(rooms, name)
			}
		}
	}
	return rooms
}

func (h *Hub) broadcastStatusToAllRooms(uid UserId, roomNames []string) {
	if len(roomNames) == 0 {
		return
	}
	h.Lock()
	state := h.mergedStateLocked(uid)
	if !state.Presence.visible() {
		h.Unlock()
		return
	}

	seen := make(map[*Conn]struct{})
	for _, name := range roomNames {
		r, ok := h.rooms[name]
		if !ok {
			continue
		}
		for memberUid := range r.members {
			for _, conn := range h.userConns[memberUid] {
				if _, inRoom := conn.rooms[name]; !inRoom {
					continue
				}
				seen[conn] = struct{}{}
			}
		}
	}
	for _, conn := range h.userConns[uid] {
		seen[conn] = struct{}{}
	}
	h.Unlock()

	payload := map[string]any{
		"cmd":        "status_update",
		"user_id":    string(uid),
		"username":   string(state.Username),
		"status":     state.Status,
		"presence":   string(state.Presence),
		"activities": state.Activities,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	for conn := range seen {
		select {
		case conn.send <- data:
		default:
		}
	}
}

func (h *Hub) broadcastToUserConns(uid UserId, cmd string, payload map[string]any) {
	h.Lock()
	conns := make([]*Conn, 0, len(h.userConns[uid]))
	conns = append(conns, h.userConns[uid]...)
	h.Unlock()

	wrapper := map[string]any{"cmd": cmd}
	maps.Copy(wrapper, payload)
	data, err := json.Marshal(wrapper)
	if err != nil {
		return
	}
	for _, c := range conns {
		select {
		case c.send <- data:
		default:
		}
	}
}

func (h *Hub) sendToUser(userId UserId, data []byte) {
	h.Lock()
	defer h.Unlock()
	for _, c := range h.userConns[userId] {
		select {
		case c.send <- data:
		default:
		}
	}
}

func (h *Hub) sweep() {
	h.Lock()
	for name, r := range h.rooms {
		if len(r.members) == 0 {
			delete(h.rooms, name)
		}
	}
	h.Unlock()
}

func (h *Hub) mergedPresenceLocked(uid UserId) Presence {
	var best Presence
	var bestTime time.Time
	for _, c := range h.userConns[uid] {
		if c.lastPresenceSet.IsZero() {
			continue
		}
		if c.lastPresenceSet.After(bestTime) {
			bestTime = c.lastPresenceSet
			best = c.presence
		}
	}
	if bestTime.IsZero() {
		return PresenceOnline
	}
	return best
}

func (h *Hub) mergedStateLocked(uid UserId) RoomMember {
	var username Username
	us := h.userStatus[uid]
	if us == nil {
		us = &UserStatus{Presence: PresenceOnline, Activities: make(map[string]Activity)}
	}
	for _, c := range h.userConns[uid] {
		if c.username != "" {
			username = c.username
			break
		}
	}
	presence := h.mergedPresenceLocked(uid)

	activities := make([]Activity, 0, len(us.Activities))
	for _, a := range us.Activities {
		activities = append(activities, a)
	}

	return RoomMember{
		UserID:     uid,
		Username:   username,
		Status:     us.Status,
		Presence:   presence,
		Activities: activities,
	}
}

func (h *Hub) getUserStatus(uid UserId) *RoomMember {
	h.Lock()
	defer h.Unlock()
	if len(h.userConns[uid]) == 0 {
		us := h.userStatus[uid]
		if us == nil {
			user := getUserById(uid)
			if user == nil {
				return nil
			}
			raw := user.Get("sys.status")
			if m, ok := raw.(map[string]any); ok {
				presenceStr := getStringOrDefault(m["presence"], "online")
				statusStr := getStringOrDefault(m["status"], "")
				us = &UserStatus{
					Presence: Presence(strings.ToLower(presenceStr)),
					Status:   statusStr,
				}
			} else {
				return nil
			}
		}
		activities := make([]Activity, 0, len(us.Activities))
		for _, a := range us.Activities {
			activities = append(activities, a)
		}
		return &RoomMember{
			UserID:     uid,
			Username:   "",
			Status:     us.Status,
			Presence:   us.Presence,
			Activities: activities,
		}
	}
	state := h.mergedStateLocked(uid)
	if !state.Presence.visible() {
		return nil
	}
	return &state
}

func roomSnapshotLocked(h *Hub, roomName string) []RoomMember {
	r, ok := h.rooms[roomName]
	if !ok {
		return nil
	}
	seen := make(map[UserId]struct{})
	members := make([]RoomMember, 0)
	for uid := range r.members {
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		state := h.mergedStateLocked(uid)
		if !state.Presence.visible() {
			continue
		}
		members = append(members, state)
	}
	return members
}

func (h *Hub) persistStatusLocked(uid UserId, us *UserStatus) {
	user := getUserById(uid)
	if user == nil {
		return
	}
	user.Set("sys.status", map[string]any{
		"presence": string(us.Presence),
		"status":   us.Status,
	})
	go saveUsers()
}

func init() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			hub.sweep()
		}
	}()
	log.Println("Status WebSocket hub started")
}
