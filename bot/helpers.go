package bot

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

// App is the Pocketbase app instance that will be injected
var App *pocketbase.PocketBase

// retrieveImgurLinks takes a message content string and returns a slice of imgur media links
func retrieveImgurLinks(content string) []string {
	matches := imgurRegexp.FindAllStringSubmatch(content, -1)

	var links []string
	for _, match := range matches {
		link := normalizeImgurLink(match)
		links = append(links, link)
	}

	return links
}

// normalizeImgurMediaLink takes an imgur link and normalizes it to the expected format
func normalizeImgurLink(imgurLink []string) string {
	prefix := imgurLink[1]    // "i." or empty
	mediaID := imgurLink[2]   // the alphanumeric ID
	extension := imgurLink[3] // ".mp4" or empty

	if prefix != "i." || extension == "" {
		return fmt.Sprintf("https://i.imgur.com/%s.mp4", mediaID)
	}

	return imgurLink[0]
}

// createMetadata takes a slice of role names and returns a Metadata struct
func createMetadata(pingRoleNames []string) Metadata {
	idolGroups := extractIdolAndGroupFromRoles(pingRoleNames)
	idolNamesStr, groupNamesStr := getCommaSeparatedIdolAndGroupNames(idolGroups)

	return Metadata{
		Idol:  idolNamesStr,
		Group: groupNamesStr,
	}
}

// extractMetadata same logic as old, just fill new fields if needed
func extractMetadata(content string, metadata *Metadata) error {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		keyValue := strings.SplitN(line, ":", 2)
		if len(keyValue) == 2 {
			key := strings.TrimSpace(keyValue[0])
			value := strings.TrimSpace(keyValue[1])

			switch key {
			case "file":
				metadata.File = value
			case "filetype":
				metadata.Filetype = value
			case "title":
				metadata.Title = value
			case "idol":
				metadata.Idol = value
			case "group":
				metadata.Group = value
			case "tags":
				metadata.Tags = value
			case "uploader":
				metadata.Uploader = value
			case "date":
				metadata.Date = value
			case "source":
				metadata.Source = value
			case "discord":
				metadata.Discord = value
			case "mirror":
				metadata.Mirror = value
			case "hqMirror":
				metadata.HqMirror = value
			case "setId":
				metadata.SetId = value
			}
		}
	}

	if len(metadata.Idol) == 0 || len(metadata.Group) == 0 {
		return fmt.Errorf("no idol or group string found")
	}

	if len(metadata.Title) == 0 {
		metadata.Title = metadata.Idol + " from " + metadata.Group
	}

	if len(metadata.Source) == 0 {
		match := youtubeRegexp.FindString(content)
		if match != "" {
			metadata.Source = match
		}
	}

	if len(metadata.HqMirror) == 0 {
		match := pixeldrainRegexp.FindString(content)
		if match != "" {
			metadata.HqMirror = match
		}
	}

	return nil
}

func createSetRecord(metadata Metadata) error {
	var date string

	if len(metadata.Date) == 6 {
		t, err := time.Parse("060102", metadata.Date)
		if err == nil {
			date = t.Format("060102")
		} else {
			date = time.Now().Format("060102")
		}
	} else {
		date = time.Now().Format("060102")
	}

	newTitle := fmt.Sprintf("%s %s", date, metadata.Title)

	groupIDs := createGroupIDSet(metadata.Group, groupMap)
	var finalGroupIDs []string
	for groupID := range groupIDs {
		finalGroupIDs = append(finalGroupIDs, groupID)
	}

	idolNames := convertToStringSlice(metadata.Idol)
	var finalIdolIDs []string
	for _, idolName := range idolNames {
		idolID, ok := getIdolIDByGroup(idolName, groupIDs)
		if ok {
			finalIdolIDs = append(finalIdolIDs, idolID)
		}
	}

	var finalUploaderIDs []string
	uploaderNames := convertToStringSlice(metadata.Uploader)
	for _, uploaderName := range uploaderNames {
		id, err := lookupOrCreateUploader(uploaderName)
		if err != nil {
			slog.Error("ERROR CREATING UPLOADER", "MSG", err)
			return err
		}
		finalUploaderIDs = append(finalUploaderIDs, id)
	}

	// Use internal Pocketbase API instead of HTTP
	collection, err := App.FindCollectionByNameOrId("contents_sets")
	if err != nil {
		slog.Error("ERROR FINDING COLLECTION", "MSG", err)
		return err
	}

	record := core.NewRecord(collection)
	record.Id = metadata.SetId
	record.Set("title", newTitle)
	record.Set("idol", finalIdolIDs)
	record.Set("group", finalGroupIDs)
	record.Set("uploader", finalUploaderIDs)

	if err := App.Save(record); err != nil {
		slog.Error("ERROR SAVING RECORD", "MSG", err)
		return err
	}

	return nil
}

// getRoleNamesFromIDs takes a slice of role IDs and returns their names, if found in the guild
func getRoleNamesFromIDs(s *discordgo.Session, guildID string, roleIDs []string) ([]string, error) {
	guildRoles, err := s.GuildRoles(guildID)
	if err != nil {
		slog.Error("UNABLE TO GET GUILD ROLES", "MSG", err)
		return nil, err
	}

	guildRoleMap := make(map[string]string, len(guildRoles))
	for _, role := range guildRoles {
		guildRoleMap[role.ID] = role.Name
	}

	roleNames := make([]string, 0, len(roleIDs))
	for _, roleID := range roleIDs {
		name, ok := guildRoleMap[roleID]
		if !ok {
			slog.Info("ROLE NOT FOUND", "MSG", name)
			return nil, fmt.Errorf("ROLE NOT FOUND: %s", roleID)
		}
		roleNames = append(roleNames, name)
	}

	return roleNames, nil
}

// extractIdolAndGroupFromRoles takes a slice of role names and extracts idol and group names
func extractIdolAndGroupFromRoles(roleNames []string) []IdolGroup {
	var result []IdolGroup

	for _, roleName := range roleNames {
		matches := roleRegexp.FindStringSubmatch(roleName)
		if len(matches) == 3 {
			result = append(result, IdolGroup{
				Idol:  matches[1],
				Group: matches[2],
			})
		}
	}

	return result
}

// getCommaSeparatedIdolAndGroupNames takes a slice of IdolGroup and returns comma-separated strings of idol and group names
func getCommaSeparatedIdolAndGroupNames(idolGroups []IdolGroup) (string, string) {
	var idolNames []string
	var groupNames []string
	idolSet := make(map[string]struct{})
	groupSet := make(map[string]struct{})

	for _, ig := range idolGroups {
		if _, exists := idolSet[ig.Idol]; !exists {
			idolSet[ig.Idol] = struct{}{}
			idolNames = append(idolNames, ig.Idol)
		}
		if _, exists := groupSet[ig.Group]; !exists {
			groupSet[ig.Group] = struct{}{}
			groupNames = append(groupNames, ig.Group)
		}
	}

	idolNamesSeparated := strings.Join(idolNames, ", ")
	groupNamesSeparated := strings.Join(groupNames, ", ")

	return idolNamesSeparated, groupNamesSeparated
}

// processMediaLinks -> uploads a single record to "contents" using internal PB API
func processMediaLinks(link, filename string, metadata Metadata) (string, error) {
	// 1) Convert metadata to the new schema fields
	metadataMap, err := metadata.parseMetadataToMap()
	if err != nil {
		slog.Error("UNABLE TO PARSE METADATA", "MSG", err)
		return "", err
	}

	// 2) Get the collection
	collection, err := App.FindCollectionByNameOrId("contents")
	if err != nil {
		slog.Error("ERROR FINDING COLLECTION", "MSG", err)
		return "", err
	}

	// 3) Create a new record
	record := core.NewRecord(collection)

	// Set all fields from metadata map
	for key, value := range metadataMap {
		// Parse JSON arrays for relation fields
		if key == "idol" || key == "group" || key == "uploader" {
			var ids []string
			if err := json.Unmarshal([]byte(value), &ids); err == nil {
				record.Set(key, ids)
			}
		} else if key == "tag" {
			var tags []string
			if err := json.Unmarshal([]byte(value), &tags); err == nil {
				record.Set(key, tags)
			}
		} else {
			record.Set(key, value)
		}
	}

	// 4) Download and attach the file
	fileData, err := downloadFile(link, filename)
	if err != nil {
		return "", err
	}

	file, err := filesystem.NewFileFromBytes(fileData, filename)
	if err != nil {
		return "", err
	}

	record.Set("file", file)

	// 5) Save the record
	if err := App.Save(record); err != nil {
		slog.Error("ERROR SAVING RECORD", "MSG", err)
		return "", err
	}

	return record.Id, nil
}

func downloadFile(link string, filename string) ([]byte, error) {
	client := &http.Client{}

	req, err := http.NewRequest("GET", link, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download file: %s", resp.Status)
	}

	fileData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if len(fileData) == 0 {
		return nil, fmt.Errorf("downloaded file is empty")
	}

	return fileData, nil
}

func createGroupIDSet(groupPlain string, groupMap map[string]string) map[string]bool {
	groups := convertToStringSlice(groupPlain)
	set := make(map[string]bool)
	for _, group := range groups {
		if groupID, ok := groupMap[group]; ok {
			set[groupID] = true
		}
	}
	return set
}

func getIdolIDByGroup(idol string, groupIDSet map[string]bool) (string, bool) {
	idols, exist := idolMap[idol]
	if !exist {
		return "", false
	}

	var idolIDs []string
	for _, idol := range idols {
		if groupIDSet[idol.Group] {
			idolIDs = append(idolIDs, idol.ID)
		}
	}

	if len(idolIDs) == 0 {
		return "", false
	} else {
		return idolIDs[0], true
	}
}

// loadGroupsFromDB loads groups from Pocketbase database
func loadGroupsFromDB() (map[string]string, error) {
	records, err := App.FindRecordsByFilter("groups", "", "-created", 0, 0)
	if err != nil {
		return nil, err
	}

	m := make(map[string]string, len(records))
	for _, record := range records {
		name := strings.ToLower(strings.TrimSpace(record.GetString("name")))
		m[name] = record.Id
	}

	return m, nil
}

// loadUploadersFromDB loads uploaders from Pocketbase database
func loadUploadersFromDB() (map[string]string, error) {
	records, err := App.FindRecordsByFilter("uploaders", "", "-created", 0, 0)
	if err != nil {
		return nil, err
	}

	m := make(map[string]string, len(records))
	for _, record := range records {
		name := strings.ToLower(strings.TrimSpace(record.GetString("name")))
		m[name] = record.Id
	}

	return m, nil
}

// loadIdolsFromDB loads idols from Pocketbase database
func loadIdolsFromDB() (map[string][]IdolItem, error) {
	records, err := App.FindRecordsByFilter("groups_idols", "", "-created", 0, 0)
	if err != nil {
		return nil, err
	}

	m := make(map[string][]IdolItem)
	for _, record := range records {
		name := strings.ToLower(strings.TrimSpace(record.GetString("name")))
		item := IdolItem{
			ID:    record.Id,
			Name:  record.GetString("name"),
			Code:  record.GetString("code"),
			Group: record.GetString("group"), // This is a relation ID to the groups collection
		}
		m[name] = append(m[name], item)
	}

	return m, nil
}


func createUploaderInPB(uploaderName string) (string, error) {
	// Use internal Pocketbase API instead of HTTP
	collection, err := App.FindCollectionByNameOrId("uploaders")
	if err != nil {
		slog.Error("ERROR FINDING COLLECTION", "MSG", err)
		return "", err
	}

	record := core.NewRecord(collection)
	record.Set("name", uploaderName)

	if err := App.Save(record); err != nil {
		slog.Error("ERROR SAVING RECORD", "MSG", err)
		return "", err
	}

	return record.Id, nil
}

func lookupOrCreateUploader(uploaderName string) (string, error) {
	id, found := uploaderMap[uploaderName]
	if found {
		return id, nil
	}

	newID, err := createUploaderInPB(uploaderName)
	if err != nil {
		slog.Error("UNABLE TO CREATE UPLOADER IN PB: ", "MSG", err)
		return "", err
	}

	uploaderMap[uploaderName] = newID

	return newID, nil
}

var lastMetadata Metadata

// convertToStringSlice takes a comma-separated string and returns a slice of trimmed strings
func convertToStringSlice(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			trimmed = strings.ToLower(trimmed)
			result = append(result, trimmed)
		}
	}

	return result
}

// parseMetadataToMap modifies how we pass data to the new "contents" collection.
func (m Metadata) parseMetadataToMap() (map[string]string, error) {
	// 1) Build a set of group IDs from m.Group
	groupIDSet := createGroupIDSet(m.Group, groupMap)
	// e.g. if "IVE, NewJeans" => { "mg12ovw2liil5j4":true, "njs999":true }

	// 2) For each idol in m.Idol, find a matching record among idolRepo
	var finalIdolIDs []string
	idolParts := convertToStringSlice(m.Idol) // ["Yujin","Hyein","Wonyoung"]
	for _, iName := range idolParts {
		idolID, ok := getIdolIDByGroup(iName, groupIDSet)
		if ok {
			finalIdolIDs = append(finalIdolIDs, idolID)
		}
	}

	// 3) Meanwhile, also store all group IDs in an array, in case your new "contents" field is multi-group
	var finalGroupIDs []string
	for gID := range groupIDSet {
		finalGroupIDs = append(finalGroupIDs, gID)
	}
	// Instead of calling namesToIDs for uploader:
	var uploaderIDs []string
	uploaderNames := convertToStringSlice(m.Uploader)
	for _, name := range uploaderNames {
		id, err := lookupOrCreateUploader(name)
		if err != nil {
			slog.Error("UNABLE TO CREATE UPLOADER: ", "MSG", err)
			return nil, err
		}
		uploaderIDs = append(uploaderIDs, id)
	}

	idolJSON, _ := json.Marshal(finalIdolIDs)
	groupJSON, _ := json.Marshal(finalGroupIDs)
	uploaderJSON, _ := json.Marshal(uploaderIDs)

	// "tag" is also an array in new PB. If you want to allow multiple tags,
	// parse m.Tags by comma and build a JSON array.
	tagsSlice := []string{}
	if m.Tags != "" {
		for _, t := range strings.Split(m.Tags, ",") {
			trimmed := strings.TrimSpace(t)
			if trimmed != "" {
				tagsSlice = append(tagsSlice, trimmed)
			}
		}
	}
	// Convert slice to JSON array string
	var tagArrayStr string
	if len(tagsSlice) == 0 {
		tagArrayStr = "[]"
	} else {
		quotedTags := []string{}
		for _, t := range tagsSlice {
			quotedTags = append(quotedTags, fmt.Sprintf("\"%s\"", t))
		}
		tagArrayStr = fmt.Sprintf("[%s]", strings.Join(quotedTags, ","))
	}

	metadataMap := map[string]string{
		// new PB "contents" fields
		"title": m.Title,
		// pass idol/group/uploader as JSON array strings
		"idol":     string(idolJSON), // e.g. ["YujinID"] if you have real IDs
		"group":    string(groupJSON),
		"uploader": string(uploaderJSON),
		"tag":      tagArrayStr,

		"filetype": m.Filetype,
		"date":     m.Date,
		"source":   m.Source,
		"discord":  m.Discord,
		"mirror":   m.Mirror,
		"hqMirror": m.HqMirror,
		// "set" is a single relation - we can pass one ID if we have a real "contents_sets" record
		"set": m.SetId,

		// Optional fields in new PB:
		"origin":    "discord-kpf", // or whatever your new schema expects
		"isQuality": "false",       // or "true" if you want
	}

	// If user typed a special date format "now" or "today", handle that
	if m.Date == "now" || m.Date == "today" {
		metadataMap["date"] = time.Now().Format(time.RFC3339Nano)
	} else {
		// If user typed YYMMDD
		if len(m.Date) == 6 { // naive check
			if parsed, err := time.Parse("060102", m.Date); err == nil {
				metadataMap["date"] = parsed.Format(time.RFC3339Nano)
			}
		}
	}
	return metadataMap, nil
}
