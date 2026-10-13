package main

import "time"

type infoParams struct {
	GetCharacterInventories bool
	GetCharacterList        bool
	GetPlayerProfile        bool
	GetPlayerStatistics     bool
	GetTitleData            bool
	GetUserAccountInfo      bool
	GetUserData             bool
	GetUserInventory        bool
	GetUserReadOnlyData     bool
	GetUserVirtualCurrency  bool
	PlayerStatisticNames    []string
	TitleDataKeys           []string
	UserDataKeys            []string
	UserReadOnlyDataKeys    []string
	ProfileConstraints      map[string]bool
}

// All game data is local to this new service. Empty collections mean no local
// grants or records; never import or fabricate Microsoft purchases or friends.
func combinedInfo(in infoParams, p titlePlayer, id identity, created bool, now time.Time) map[string]any {
	account := map[string]any{"PlayFabId": p.PlayFabID, "Created": p.Created,
		"NintendoSwitchAccountInfo": map[string]string{"NintendoSwitchAccountSubjectId": id.BaasUserID},
		"TitleInfo":                 map[string]any{"TitlePlayerAccount": entityKey{p.EntityID, "title_player_account"}, "Created": p.Created, "FirstLogin": p.Created, "LastLogin": now, "DisplayName": id.Nickname}}
	// AccountInfo is always retrieved in GetPlayerCombinedInfoResultPayload.
	out := map[string]any{"AccountInfo": account}
	for name, requested := range map[string]bool{"CharacterInventories": in.GetCharacterInventories, "CharacterList": in.GetCharacterList, "PlayerStatistics": in.GetPlayerStatistics, "UserInventory": in.GetUserInventory} {
		if requested {
			out[name] = []any{}
		}
	}
	for name, requested := range map[string]bool{"TitleData": in.GetTitleData, "UserData": in.GetUserData, "UserReadOnlyData": in.GetUserReadOnlyData, "UserVirtualCurrency": in.GetUserVirtualCurrency} {
		if requested {
			out[name] = map[string]any{}
		}
	}
	if in.GetUserData {
		out["UserDataVersion"] = 0
	}
	if in.GetUserReadOnlyData {
		out["UserReadOnlyDataVersion"] = 0
	}
	if in.GetUserVirtualCurrency {
		out["UserVirtualCurrencyRechargeTimes"] = map[string]any{}
	}
	if in.GetPlayerProfile && !created {
		profile := map[string]any{"PlayerId": p.PlayFabID, "TitleId": p.TitleID}
		if in.ProfileConstraints == nil || in.ProfileConstraints["ShowDisplayName"] {
			profile["DisplayName"] = id.Nickname
		}
		if in.ProfileConstraints["ShowCreated"] {
			profile["Created"] = p.Created
		}
		if in.ProfileConstraints["ShowLastLogin"] {
			profile["LastLogin"] = now
		}
		out["PlayerProfile"] = profile
	}
	return out
}
