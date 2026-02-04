package main

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func getMyGroups(c *gin.Context) {
	user := c.MustGet("user").(*User)

	id := user.GetId()

	groupsDataMutex.RLock()
	defer groupsDataMutex.RUnlock()

	outGroups := make([]GroupPublic, 0)

	for _, data := range groupsData {
		for _, member := range data.Members {
			if member.UserId == id {
				publicGroup := data.Group.ToPublic()
				publicGroup.MemberCount = len(data.Members)
				outGroups = append(outGroups, publicGroup)
				break
			}
		}
	}

	c.JSON(200, outGroups)
}

func createGroup(c *gin.Context) {
	user := c.MustGet("user").(*User)

	tag := c.Query("tag")
	if tag == "" {
		c.JSON(400, gin.H{"error": "Tag is required"})
		return
	}
	if len(tag) > 20 {
		c.JSON(400, gin.H{"error": "Tag length exceeded"})
		return
	}
	re := regexp.MustCompile(`^[a-zA-Z0-9]+$`)
	if !re.MatchString(tag) {
		c.JSON(400, gin.H{"error": "Tag must be alphanumeric only"})
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
	if len(description) > 500 {
		c.JSON(400, gin.H{"error": "Description length exceeded"})
		return
	}
	iconUrl := c.Query("icon_url")
	if iconUrl != "" && !strings.HasPrefix(iconUrl, "http://") && !strings.HasPrefix(iconUrl, "https://") {
		c.JSON(400, gin.H{"error": "Icon must be a valid URL"})
		return
	}
	bannerUrl := c.Query("banner_url")
	if bannerUrl != "" && !strings.HasPrefix(bannerUrl, "http://") && !strings.HasPrefix(bannerUrl, "https://") {
		c.JSON(400, gin.H{"error": "Banner must be a valid URL"})
		return
	}

	public := c.DefaultQuery("public", "false") == "true"
	joinPolicyRaw := c.DefaultQuery("join_policy", "OPEN")
	joinPolicy := JoinPolicy(joinPolicyRaw)
	if joinPolicy != JoinPolicyOpen && joinPolicy != JoinPolicyRequest && joinPolicy != JoinPolicyInvite {
		c.JSON(400, gin.H{"error": "Invalid join policy"})
		return
	}

	_, exists := getGroupByTag(tag)
	if exists {
		c.JSON(400, gin.H{"error": "Group with this tag already exists"})
		return
	}

	ownerId := user.GetId()
	groupsDataMutex.RLock()
	for _, data := range groupsData {
		if data.Group.OwnerUserId == ownerId {
			groupsDataMutex.RUnlock()
			c.JSON(400, gin.H{"error": "You already own a group"})
			return
		}
	}
	groupsDataMutex.RUnlock()

	groupId := GroupId(uuid.New().String())
	group := Group{
		Id:             groupId,
		Tag:            tag,
		Name:           name,
		Description:    description,
		IconUrl:        iconUrl,
		BannerUrl:      bannerUrl,
		OwnerUserId:    user.GetId(),
		Public:         public,
		JoinPolicy:     joinPolicy,
		CreatedAt:      time.Now().Unix(),
		CreditsBalance: 0,
	}

	memberId := uuid.New().String()
	ownerRoleId := uuid.New().String()

	allPermissions := []string{
		"groups.manage",
		"groups.members.invite",
		"groups.members.remove",
		"groups.roles.manage",
		"groups.roles.assign",
		"groups.announcements.send",
		"groups.events.manage",
		"groups.events.publish",
		"groups.tips.manage",
		"groups.group.edit",
	}

	ownerRole := GroupRole{
		Id:             ownerRoleId,
		GroupTag:       tag,
		Name:           "Owner",
		Description:    "Group owner",
		AssignOnJoin:   false,
		SelfAssignable: false,
		Benefits:       []string{},
		Permissions:    allPermissions,
	}

	memberRole := GroupRole{
		Id:             memberId,
		GroupTag:       tag,
		Name:           "Member",
		Description:    "Regular group member",
		AssignOnJoin:   true,
		SelfAssignable: false,
		Benefits:       []string{},
		Permissions:    []string{},
	}

	roles := []GroupRole{ownerRole, memberRole}

	newGroupData := GroupData{
		Group: group,
		Members: []GroupMember{
			{
				Id:                 uuid.New().String(),
				GroupTag:           tag,
				UserId:             ownerId,
				RoleIds:            []string{ownerRoleId, memberId},
				JoinedAt:           time.Now().Unix(),
				MutedAnnouncements: false,
			},
		},
		Roles:           roles,
		Announcements:   []GroupAnnouncement{},
		Events:          map[string]GroupEvent{},
		Tips:            []GroupTip{},
		BenefitProducts: map[string]GroupBenefitProduct{},
	}

	groupsDataMutex.Lock()
	groupsData[tag] = &newGroupData
	groupsDataMutex.Unlock()
	go saveGroupData(tag)

	log.Printf("Created group '%s' with tag '%s'", name, tag)
	log.Printf("Groups in memory: %d", len(groupsData))

	netGroup := group.ToNet()
	netGroup.MemberCount = 1

	c.JSON(201, netGroup)
}

func searchGroups(c *gin.Context) {
	query := c.Query("query")
	if query == "" {
		c.JSON(400, gin.H{"error": "Query is required"})
		return
	}

	groupsDataMutex.RLock()
	defer groupsDataMutex.RUnlock()

	var results []GroupNet
	for _, data := range groupsData {
		if !data.Group.Public {
			continue
		}
		if strings.Contains(strings.ToLower(data.Group.Name), strings.ToLower(query)) ||
			strings.Contains(strings.ToLower(data.Group.Description), strings.ToLower(query)) {
			netGroup := data.Group.ToNet()
			netGroup.MemberCount = len(data.Members)
			results = append(results, netGroup)
		}
	}

	c.JSON(200, results)
}

func joinGroup(c *gin.Context) {
	user := c.MustGet("user").(*User)

	groupTag := c.Param("grouptag")
	if groupTag == "" {
		c.JSON(400, gin.H{"error": "Group tag is required"})
		return
	}

	group, ok := getGroupByTag(groupTag)

	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	if !group.Public {
		c.JSON(403, gin.H{"error": "Group is private"})
		return
	}

	members := getGroupMembers(groupTag)
	alreadyMember := false
	id := user.GetId()
	for _, member := range members {
		if member.UserId == id {
			alreadyMember = true
			break
		}
	}

	if alreadyMember {
		c.JSON(400, gin.H{"error": "You are already a member of this group"})
		return
	}

	if group.JoinPolicy == JoinPolicyInvite {
		c.JSON(403, gin.H{"error": "This group is invite-only"})
		return
	}

	if group.JoinPolicy == JoinPolicyRequest {
		c.JSON(400, gin.H{"error": "Join requests not yet implemented"})
		return
	}

	memberId := ""
	roles := getGroupRoles(groupTag)
	for _, role := range roles {
		if role.Name == "Member" {
			memberId = role.Id
			break
		}
	}

	if memberId == "" {
		c.JSON(500, gin.H{"error": "Default member role not found"})
		return
	}

	member := GroupMember{
		Id:                 uuid.New().String(),
		GroupTag:           groupTag,
		UserId:             user.GetId(),
		RoleIds:            []string{memberId},
		JoinedAt:           time.Now().Unix(),
		MutedAnnouncements: false,
	}

	members = append(members, member)
	updateGroupMembers(groupTag, members)
	netGroup := group.ToNet()
	netGroup.MemberCount = len(members)
	c.JSON(200, netGroup)
}

func leaveGroup(c *gin.Context) {
	user := c.MustGet("user").(*User)

	groupTag := c.Param("grouptag")
	if groupTag == "" {
		c.JSON(400, gin.H{"error": "Group tag is required"})
		return
	}

	group, ok := getGroupByTag(groupTag)

	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	if user.GetId() == group.OwnerUserId {
		c.JSON(400, gin.H{"error": "You cannot leave the group you own"})
		return
	}

	members := getGroupMembers(groupTag)
	newMembers := make([]GroupMember, 0)
	hasMember := false

	for _, member := range members {
		if member.UserId == user.GetId() {
			hasMember = true
			continue
		}
		newMembers = append(newMembers, member)
	}

	if !hasMember {
		c.JSON(400, gin.H{"error": "You are not a member of this group"})
		return
	}

	updateGroupMembers(groupTag, newMembers)
	netGroup := group.ToNet()
	netGroup.MemberCount = len(newMembers)
	c.JSON(200, netGroup)
}

func getGroup(c *gin.Context) {
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

	groupData, ok := getGroupDataByTag(groupTag)
	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	publicData := groupData.Group.ToPublic()
	publicData.MemberCount = len(groupData.Members)

	c.JSON(200, publicData)
}

func updateGroup(c *gin.Context) {
	user := c.MustGet("user").(*User)

	groupTag := c.Param("grouptag")
	if groupTag == "" {
		c.JSON(400, gin.H{"error": "Group tag is required"})
		return
	}

	var jsonBody map[string]any
	if err := c.ShouldBindJSON(&jsonBody); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}

	_, ok := getGroupByTag(groupTag)

	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	groupsDataMutex.Lock()
	defer groupsDataMutex.Unlock()

	data := groupsData[groupTag]
	if data.Group.OwnerUserId != user.GetId() {
		c.JSON(403, gin.H{"error": "You are not authorized to update this group"})
		return
	}

	if description, ok := jsonBody["description"].(string); ok {
		data.Group.Description = description
	}
	if icon, ok := jsonBody["icon"].(string); ok {
		data.Group.IconUrl = icon
	}
	if banner_url, ok := jsonBody["banner_url"].(string); ok {
		data.Group.BannerUrl = banner_url
	}
	if public, ok := jsonBody["public"].(bool); ok {
		data.Group.Public = public
	}
	if joinPolicy, ok := jsonBody["join_policy"].(string); ok {
		data.Group.JoinPolicy = JoinPolicy(joinPolicy)
	}

	groupsData[groupTag] = data
	go saveGroupData(groupTag)

	netGroup := data.Group.ToNet()
	netGroup.MemberCount = len(data.Members)
	c.JSON(200, netGroup)
}

func deleteGroup(c *gin.Context) {
	user := c.MustGet("user").(*User)

	groupTag := c.Param("grouptag")
	if groupTag == "" {
		c.JSON(400, gin.H{"error": "Group tag is required"})
		return
	}

	group, ok := getGroupByTag(groupTag)

	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	groupsDataMutex.Lock()
	defer groupsDataMutex.Unlock()

	if group.OwnerUserId != user.GetId() {
		c.JSON(403, gin.H{"error": "You are not authorized to delete this group"})
		return
	}

	delete(groupsData, groupTag)
	go deleteGroupData(groupTag)

	c.JSON(200, gin.H{"message": "Group deleted successfully"})
}

func representGroup(c *gin.Context) {
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
	isMember := false
	for _, member := range members {
		if member.UserId == user.GetId() {
			isMember = true
			break
		}
	}

	if !isMember {
		c.JSON(400, gin.H{"error": "You are not a member of this group"})
		return
	}

	data, _ := getGroupDataByTag(groupTag)
	user.Set("sys.group", data.Group.Id)
	go saveUsers()

	c.JSON(200, gin.H{"message": "You are now representing this group"})
}

func disrepresentGroup(c *gin.Context) {
	user := c.MustGet("user").(*User)

	user.DelKey("sys.group")
	go saveUsers()

	c.JSON(200, gin.H{"message": "You are no longer representing any group"})
}

func reportGroup(c *gin.Context) {
	user := c.MustGet("user").(*User)

	groupTag := c.Param("grouptag")
	if groupTag == "" {
		c.JSON(400, gin.H{"error": "Group tag is required"})
		return
	}

	group, ok := getGroupByTag(groupTag)

	if !ok {
		c.JSON(404, gin.H{"error": "Group not found"})
		return
	}

	reportStr := fmt.Sprintf("Reported by %s\nGroup: %s (%s)\nDescription: %s\n%s", user.GetUsername(), group.Name, groupTag, group.Description, JSONStringify(group))

	sendReportToDiscord(reportStr)

	c.JSON(200, gin.H{"message": "Report sent successfully"})
}

func hasPermission(userId UserId, groupTag string, permission string) bool {
	members := getGroupMembers(groupTag)

	for _, member := range members {
		if member.UserId == userId {
			rolesMap := getGroupRolesMap(groupTag)
			for _, roleId := range member.RoleIds {
				role, roleExists := rolesMap[roleId]
				if roleExists {
					if role.Name == "Owner" {
						return true
					}
					for _, perm := range role.Permissions {
						if perm == permission {
							return true
						}
					}
				}
			}
		}
	}
	return false
}
