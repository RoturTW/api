package main

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func createAnnouncement(c *gin.Context) {
	user := c.MustGet("user").(*User)

	groupTag := c.Param("grouptag")
	if groupTag == "" {
		c.JSON(400, gin.H{"error": "Group tag is required"})
		return
	}

	title := c.Query("title")
	if title == "" {
		c.JSON(400, gin.H{"error": "Title is required"})
		return
	}
	if len(title) > 100 {
		c.JSON(400, gin.H{"error": "Title length exceeded"})
		return
	}

	body := c.Query("body")
	if len(body) > 2000 {
		c.JSON(400, gin.H{"error": "Body length exceeded"})
		return
	}

	_, ok := getGroupByTag(groupTag)
	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	if !hasPermission(user.GetId(), groupTag, "groups.announcements.send") {
		c.JSON(403, gin.H{"error": "You don't have permission to send announcements"})
		return
	}

	pingMembers := c.DefaultQuery("ping_members", "false") == "true"

	announcement := GroupAnnouncement{
		Id:           uuid.New().String(),
		GroupTag:     groupTag,
		Title:        title,
		Body:         body,
		AuthorUserId: user.GetId(),
		CreatedAt:    time.Now().Unix(),
		PingMembers:  pingMembers,
	}

	addGroupAnnouncement(groupTag, announcement)

	c.JSON(201, announcement)
}

func getAnnouncements(c *gin.Context) {
	groupTag := c.Param("grouptag")
	if groupTag == "" {
		c.JSON(400, gin.H{"error": "Group tag is required"})
		return
	}

	_, ok := getGroupByTag(groupTag)
	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	limitStr := c.DefaultQuery("limit", "10")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 10
	}

	announcements := getGroupAnnouncements(groupTag)

	var results []GroupAnnouncement
	count := 0
	for i := len(announcements) - 1; i >= 0 && count < limit; i-- {
		if announcements[i].GroupTag == groupTag {
			results = append(results, announcements[i])
			count++
		}
	}

	c.JSON(200, results)
}

func deleteAnnouncement(c *gin.Context) {
	user := c.MustGet("user").(*User)

	groupTag := c.Param("grouptag")
	announcementId := c.Param("announcementid")

	if groupTag == "" || announcementId == "" {
		c.JSON(400, gin.H{"error": "Group tag and announcement ID are required"})
		return
	}

	_, ok := getGroupByTag(groupTag)
	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	if !hasPermission(user.GetId(), groupTag, "groups.announcements.send") {
		c.JSON(403, gin.H{"error": "You don't have permission to delete announcements"})
		return
	}

	announcements := getGroupAnnouncements(groupTag)
	newAnnouncements := make([]GroupAnnouncement, 0)
	found := false

	for _, ann := range announcements {
		if ann.Id == announcementId && ann.GroupTag == groupTag {
			found = true
			continue
		}
		newAnnouncements = append(newAnnouncements, ann)
	}

	if !found {
		c.JSON(404, gin.H{"error": "Announcement not found"})
		return
	}

	groupsDataMutex.Lock()
	data := groupsData[groupTag]
	data.Announcements = newAnnouncements
	groupsData[groupTag] = data
	groupsDataMutex.Unlock()

	go saveGroupData(groupTag)

	c.JSON(200, gin.H{"message": "Announcement deleted"})
}

func createEvent(c *gin.Context) {
	user := c.MustGet("user").(*User)

	groupTag := c.Param("grouptag")
	if groupTag == "" {
		c.JSON(400, gin.H{"error": "Group tag is required"})
		return
	}

	title := c.Query("title")
	if title == "" {
		c.JSON(400, gin.H{"error": "Title is required"})
		return
	}
	if len(title) > 100 {
		c.JSON(400, gin.H{"error": "Title length exceeded"})
		return
	}

	description := c.Query("description")
	if len(description) > 500 {
		c.JSON(400, gin.H{"error": "Description length exceeded"})
		return
	}

	location := c.Query("location")
	if len(location) > 200 {
		c.JSON(400, gin.H{"error": "Location length exceeded"})
		return
	}

	_, ok := getGroupByTag(groupTag)
	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	if !hasPermission(user.GetId(), groupTag, "groups.events.manage") {
		c.JSON(403, gin.H{"error": "You don't have permission to manage events"})
		return
	}

	startTimeStr := c.Query("start_time")
	durationHoursStr := c.DefaultQuery("duration_hours", "1")

	startTime, err := strconv.ParseInt(startTimeStr, 10, 64)
	if err != nil || startTime <= time.Now().Unix() {
		c.JSON(400, gin.H{"error": "Invalid start time"})
		return
	}

	durationHours, err := strconv.Atoi(durationHoursStr)
	if err != nil || durationHours <= 0 || durationHours > 72 {
		c.JSON(400, gin.H{"error": "Invalid duration (must be 1-72 hours)"})
		return
	}

	endTime := startTime + (int64(durationHours) * 3600)

	visibilityRaw := c.DefaultQuery("visibility", "MEMBERS")
	visibility := EventVisibility(visibilityRaw)
	if visibility != EventVisibilityMembers && visibility != EventVisibilityPublic {
		c.JSON(400, gin.H{"error": "Invalid visibility"})
		return
	}

	published := c.DefaultQuery("published", "false") == "true"
	if published {
		if !hasPermission(user.GetId(), groupTag, "groups.events.publish") {
			c.JSON(403, gin.H{"error": "You don't have permission to publish events"})
			return
		}
	}

	eventId := uuid.New().String()
	event := GroupEvent{
		Id:          eventId,
		GroupTag:    groupTag,
		Title:       title,
		Description: description,
		StartTime:   startTime,
		EndTime:     endTime,
		Location:    location,
		Visibility:  visibility,
		CreatedBy:   user.GetId(),
		Published:   published,
	}

	addGroupEvent(groupTag, event)

	c.JSON(201, event)
}

func getEvents(c *gin.Context) {
	groupTag := c.Param("grouptag")
	if groupTag == "" {
		c.JSON(400, gin.H{"error": "Group tag is required"})
		return
	}

	_, ok := getGroupByTag(groupTag)
	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	user := c.MustGet("user").(*User)

	events := getGroupEvents(groupTag)
	members := getGroupMembers(groupTag)

	isMember := false
	for _, member := range members {
		if member.UserId == user.GetId() {
			isMember = true
			break
		}
	}

	var results []GroupEvent
	for _, event := range events {
		if event.GroupTag == groupTag {
			switch event.Visibility {
			case EventVisibilityPublic:
				results = append(results, event)
			case EventVisibilityMembers:
				if isMember {
					results = append(results, event)
				}
			}
		}
	}

	c.JSON(200, results)
}

func sendTip(c *gin.Context) {
	user := c.MustGet("user").(*User)

	groupTag := c.Param("grouptag")
	if groupTag == "" {
		c.JSON(400, gin.H{"error": "Group tag is required"})
		return
	}

	amountStr := c.Query("amount")
	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil || amount <= 0 {
		c.JSON(400, gin.H{"error": "Invalid amount"})
		return
	}

	group, ok := getGroupByTag(groupTag)
	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	members := getGroupMembers(groupTag)
	isMember := false
	for _, member := range members {
		if member.UserId == user.GetId() {
			isMember = true
			break
		}
	}

	if !isMember && !group.Public {
		c.JSON(403, gin.H{"error": "You can only tip groups you're a member of"})
		return
	}

	tip := GroupTip{
		Id:            uuid.New().String(),
		GroupTag:      groupTag,
		FromUserId:    user.GetId(),
		AmountCredits: amount,
		CreatedAt:     time.Now().Unix(),
	}

	addGroupTip(groupTag, tip)

	c.JSON(201, tip)
}

func getTips(c *gin.Context) {
	groupTag := c.Param("grouptag")
	if groupTag == "" {
		c.JSON(400, gin.H{"error": "Group tag is required"})
		return
	}

	_, ok := getGroupByTag(groupTag)
	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	limitStr := c.DefaultQuery("limit", "20")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 20
	}

	tips := getGroupTips(groupTag)

	var results []GroupTip
	count := 0
	for i := len(tips) - 1; i >= 0 && count < limit; i-- {
		if tips[i].GroupTag == groupTag {
			results = append(results, tips[i])
			count++
		}
	}

	c.JSON(200, results)
}

func createRole(c *gin.Context) {
	user := c.MustGet("user").(*User)

	groupTag := c.Param("grouptag")
	if groupTag == "" {
		c.JSON(400, gin.H{"error": "Group tag is required"})
		return
	}

	name := c.Query("name")
	if name == "" {
		c.JSON(400, gin.H{"error": "Name is required"})
		return
	}
	if len(name) > 50 {
		c.JSON(400, gin.H{"error": "Name length exceeded"})
		return
	}

	description := c.Query("description")
	if len(description) > 200 {
		c.JSON(400, gin.H{"error": "Description length exceeded"})
		return
	}

	assignOnJoin := c.DefaultQuery("assign_on_join", "false") == "true"
	selfAssignable := c.DefaultQuery("self_assignable", "false") == "true"

	_, ok := getGroupByTag(groupTag)
	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	if !hasPermission(user.GetId(), groupTag, "groups.roles.manage") {
		c.JSON(403, gin.H{"error": "You don't have permission to manage roles"})
		return
	}

	roleId := uuid.New().String()
	role := GroupRole{
		Id:             roleId,
		GroupTag:       groupTag,
		Name:           name,
		Description:    description,
		AssignOnJoin:   assignOnJoin,
		SelfAssignable: selfAssignable,
		Benefits:       []string{},
		Permissions:    []string{},
	}

	roles := getGroupRoles(groupTag)
	roles = append(roles, role)
	updateGroupRoles(groupTag, roles)

	c.JSON(201, role)
}

func getRoles(c *gin.Context) {
	groupTag := c.Param("grouptag")
	if groupTag == "" {
		c.JSON(400, gin.H{"error": "Group tag is required"})
		return
	}

	_, ok := getGroupByTag(groupTag)
	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	roles := getGroupRoles(groupTag)

	var results []GroupRole
	for _, role := range roles {
		if role.GroupTag == groupTag {
			results = append(results, role)
		}
	}

	c.JSON(200, results)
}

func updateRole(c *gin.Context) {
	user := c.MustGet("user").(*User)

	groupTag := c.Param("grouptag")
	roleId := c.Param("roleid")

	if groupTag == "" || roleId == "" {
		c.JSON(400, gin.H{"error": "Group tag and role ID are required"})
		return
	}

	_, ok := getGroupByTag(groupTag)
	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	if !hasPermission(user.GetId(), groupTag, "groups.roles.manage") {
		c.JSON(403, gin.H{"error": "You don't have permission to manage roles"})
		return
	}

	var updateData map[string]any
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}

	roles := getGroupRoles(groupTag)
	found := false

	for i, role := range roles {
		if role.Id == roleId && role.GroupTag == groupTag {
			found = true
			if name, ok := updateData["name"]; ok {
				roles[i].Name = name.(string)
			}
			if description, ok := updateData["description"]; ok {
				roles[i].Description = description.(string)
			}
			if assignOnJoin, ok := updateData["assign_on_join"]; ok {
				roles[i].AssignOnJoin = assignOnJoin.(bool)
			}
			if selfAssignable, ok := updateData["self_assignable"]; ok {
				roles[i].SelfAssignable = selfAssignable.(bool)
			}
			if permissions, ok := updateData["permissions"]; ok {
				roles[i].Permissions = permissions.([]string)
			}
			if benefits, ok := updateData["benefits"]; ok {
				roles[i].Benefits = benefits.([]string)
			}
			break
		}
	}

	if !found {
		c.JSON(404, gin.H{"error": "Role not found"})
		return
	}

	updateGroupRoles(groupTag, roles)

	c.JSON(200, gin.H{"message": "Role updated"})
}

func deleteRole(c *gin.Context) {
	user := c.MustGet("user").(*User)

	groupTag := c.Param("grouptag")
	roleId := c.Param("roleid")

	if groupTag == "" || roleId == "" {
		c.JSON(400, gin.H{"error": "Group tag and role ID are required"})
		return
	}

	_, ok := getGroupByTag(groupTag)
	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	if !hasPermission(user.GetId(), groupTag, "groups.roles.manage") {
		c.JSON(403, gin.H{"error": "You don't have permission to manage roles"})
		return
	}

	roles := getGroupRoles(groupTag)
	found := false

	newRoles := make([]GroupRole, 0)
	for _, role := range roles {
		if role.Id == roleId && role.GroupTag == groupTag {
			found = true
			if role.Name == "Owner" || role.Name == "Everyone" {
				c.JSON(400, gin.H{"error": "Cannot delete default roles"})
				return
			}
			continue
		}
		newRoles = append(newRoles, role)
	}

	if !found {
		c.JSON(404, gin.H{"error": "Role not found"})
		return
	}

	updateGroupRoles(groupTag, newRoles)

	c.JSON(200, gin.H{"message": "Role deleted"})
}

func getUserPermissions(c *gin.Context) {
	groupTag := c.Param("grouptag")
	targetUserId := c.Param("userid")

	if groupTag == "" || targetUserId == "" {
		c.JSON(400, gin.H{"error": "Group tag and user ID are required"})
		return
	}

	_, ok := getGroupByTag(groupTag)
	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	members := getGroupMembers(groupTag)
	var member GroupMember
	found := false

	for _, m := range members {
		if string(m.UserId) == targetUserId {
			member = m
			found = true
			break
		}
	}

	if !found {
		c.JSON(404, gin.H{"error": "User is not a member of this group"})
		return
	}

	rolesMap := getGroupRolesMap(groupTag)

	permissionsMap := make(map[string]bool)
	for _, roleId := range member.RoleIds {
		role, roleExists := rolesMap[roleId]
		if roleExists {
			for _, perm := range role.Permissions {
				permissionsMap[perm] = true
			}
		}
	}

	permissions := make([]string, 0, len(permissionsMap))
	for perm := range permissionsMap {
		permissions = append(permissions, perm)
	}

	c.JSON(200, gin.H{"permissions": permissions})
}

func getUserRoles(c *gin.Context) {
	groupTag := c.Param("grouptag")
	targetUserId := c.Param("userid")

	if groupTag == "" || targetUserId == "" {
		c.JSON(400, gin.H{"error": "Group tag and user ID are required"})
		return
	}

	_, ok := getGroupByTag(groupTag)
	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	members := getGroupMembers(groupTag)
	var member GroupMember
	found := false

	for _, m := range members {
		if string(m.UserId) == targetUserId {
			member = m
			found = true
			break
		}
	}

	if !found {
		c.JSON(404, gin.H{"error": "User is not a member of this group"})
		return
	}

	rolesMap := getGroupRolesMap(groupTag)

	var roles []GroupRole
	for _, roleId := range member.RoleIds {
		if role, roleExists := rolesMap[roleId]; roleExists {
			roles = append(roles, role)
		}
	}

	c.JSON(200, gin.H{"roles": roles})
}

func getUserBenefits(c *gin.Context) {
	groupTag := c.Param("grouptag")
	targetUserId := c.Param("userid")

	if groupTag == "" || targetUserId == "" {
		c.JSON(400, gin.H{"error": "Group tag and user ID are required"})
		return
	}

	_, ok := getGroupByTag(groupTag)
	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	members := getGroupMembers(groupTag)
	var member GroupMember
	found := false

	for _, m := range members {
		if string(m.UserId) == targetUserId {
			member = m
			found = true
			break
		}
	}

	if !found {
		c.JSON(404, gin.H{"error": "User is not a member of this group"})
		return
	}

	rolesMap := getGroupRolesMap(groupTag)

	benefitsMap := make(map[string]bool)
	for _, roleId := range member.RoleIds {
		role, roleExists := rolesMap[roleId]
		if roleExists {
			for _, benefit := range role.Benefits {
				benefitsMap[benefit] = true
			}
		}
	}

	benefits := make([]string, 0, len(benefitsMap))
	for benefit := range benefitsMap {
		benefits = append(benefits, benefit)
	}

	c.JSON(200, gin.H{"benefits": benefits})
}

func toggleAnnouncementMute(c *gin.Context) {
	user := c.MustGet("user").(*User)

	groupTag := c.Param("grouptag")
	if groupTag == "" {
		c.JSON(400, gin.H{"error": "Group tag is required"})
		return
	}

	_, ok := getGroupByTag(groupTag)
	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	members := getGroupMembers(groupTag)
	found := false

	for i, member := range members {
		if member.UserId == user.GetId() {
			found = true
			members[i].MutedAnnouncements = !members[i].MutedAnnouncements
			break
		}
	}

	if !found {
		c.JSON(400, gin.H{"error": "You are not a member of this group"})
		return
	}

	updateGroupMembers(groupTag, members)
	c.JSON(200, gin.H{"message": "Mute status updated"})
}

func assignRole(c *gin.Context) {
	user := c.MustGet("user").(*User)

	groupTag := c.Param("grouptag")
	targetUserId := c.Param("userid")
	roleId := c.Param("roleid")

	if groupTag == "" || targetUserId == "" || roleId == "" {
		c.JSON(400, gin.H{"error": "Group tag, user ID, and role ID are required"})
		return
	}

	_, ok := getGroupByTag(groupTag)
	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	rolesMap := getGroupRolesMap(groupTag)
	role, roleExists := rolesMap[roleId]

	if !roleExists || role.GroupTag != groupTag {
		c.JSON(404, gin.H{"error": "Role not found"})
		return
	}

	if role.SelfAssignable && string(user.GetId()) == targetUserId {
	} else if !hasPermission(user.GetId(), groupTag, "groups.roles.assign") {
		c.JSON(403, gin.H{"error": "You don't have permission to assign roles"})
		return
	}

	members := getGroupMembers(groupTag)
	found := false

	for i, member := range members {
		if string(member.UserId) == targetUserId {
			found = true
			for _, rId := range member.RoleIds {
				if rId == roleId {
					c.JSON(400, gin.H{"error": "User already has this role"})
					return
				}
			}
			members[i].RoleIds = append(members[i].RoleIds, roleId)
			break
		}
	}

	if !found {
		c.JSON(404, gin.H{"error": "User is not a member of this group"})
		return
	}

	updateGroupMembers(groupTag, members)

	c.JSON(200, gin.H{"message": "Role assigned"})
}

func removeRole(c *gin.Context) {
	user := c.MustGet("user").(*User)

	groupTag := c.Param("grouptag")
	targetUserId := c.Param("userid")
	roleId := c.Param("roleid")

	if groupTag == "" || targetUserId == "" || roleId == "" {
		c.JSON(400, gin.H{"error": "Group tag, user ID, and role ID are required"})
		return
	}

	_, ok := getGroupByTag(groupTag)
	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	if !hasPermission(user.GetId(), groupTag, "groups.roles.assign") {
		c.JSON(403, gin.H{"error": "You don't have permission to remove roles"})
		return
	}

	rolesMap := getGroupRolesMap(groupTag)
	role, roleExists := rolesMap[roleId]

	if !roleExists || role.GroupTag != groupTag {
		c.JSON(404, gin.H{"error": "Role not found"})
		return
	}

	if role.Name == "Owner" {
		c.JSON(400, gin.H{"error": "Cannot remove Owner role"})
		return
	}

	members := getGroupMembers(groupTag)
	found := false

	for i, member := range members {
		if string(member.UserId) == targetUserId {
			found = true
			newRoleIds := make([]string, 0)
			for _, rId := range member.RoleIds {
				if rId != roleId {
					newRoleIds = append(newRoleIds, rId)
				}
			}
			if len(newRoleIds) == len(member.RoleIds) {
				c.JSON(400, gin.H{"error": "User doesn't have this role"})
				return
			}
			members[i].RoleIds = newRoleIds
			break
		}
	}

	if !found {
		c.JSON(404, gin.H{"error": "User is not a member of this group"})
		return
	}

	updateGroupMembers(groupTag, members)

	c.JSON(200, gin.H{"message": "Role removed"})
}
