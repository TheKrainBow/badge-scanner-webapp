package service

import (
	"sort"
	"strconv"
	"strings"

	"badgescanner/backend/internal/intraclient"
	"badgescanner/backend/internal/store"
)

// UserRow mirrors ScanViewModel.UserRow: a CA user row enriched with cached
// intra info + scan stats. A "user" here is a unique login — several CA
// entries (several badges) for the same login collapse into one row.
type UserRow struct {
	Entry          store.CADirEntry `json:"entry"`
	Login          string           `json:"login"`
	ScanCount      int              `json:"scanCount"`
	PendingCount   int              `json:"pendingCount"`
	LastScan       *int64           `json:"lastScan,omitempty"`
	UserType       string           `json:"userType"`
	CoalitionName  string           `json:"coalitionName"`
	CoalitionColor string           `json:"coalitionColor"`
	PhotoURL       string           `json:"photoUrl"`
	HasError       bool             `json:"hasError"`
}

type UserFilters struct {
	Query       string
	Type        string // "Student" | "Piscine" | ""
	ScannedOnly bool
	ErrorOnly   bool
	Coalition   string
	Order       string // "alphabetical" | "latest" | "blames"
}

func groupKey(e store.CADirEntry, info *store.IntraInfo) string {
	login := e.DisplayLogin()
	if info != nil && info.Login != nil && *info.Login != "" {
		login = *info.Login
	}
	if login != "" {
		return "login:" + strings.ToLower(login)
	}
	if e.FTId != "" {
		return "id:" + e.FTId
	}
	return "pk:" + strconv.FormatInt(e.PK, 10)
}

// AllUserRows ports ScanViewModel.allUserRows: groups CA entries by resolved
// identity, merging badges of the same person into a single row.
func (s *Service) AllUserRows() ([]UserRow, error) {
	entries, err := s.Store.ListCADirectory()
	if err != nil {
		return nil, err
	}
	cache, err := s.Store.AllIntra()
	if err != nil {
		return nil, err
	}
	infoFor := func(e store.CADirEntry) *store.IntraInfo {
		key := strings.ToLower(e.IntraKey())
		if key == "" {
			return nil
		}
		if info, ok := cache[key]; ok {
			return &info
		}
		return nil
	}

	groups := map[string][]store.CADirEntry{}
	var order []string
	for _, e := range entries {
		k := groupKey(e, infoFor(e))
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], e)
	}

	var rows []UserRow
	for _, k := range order {
		group := groups[k]
		rep := group[0]
		repScore := -1
		var info *store.IntraInfo
		for _, e := range group {
			score := 0
			if e.FTId != "" {
				score += 2
			}
			if e.FTLogin != "" {
				score += 1
			}
			if score > repScore {
				repScore = score
				rep = e
			}
			if info == nil {
				info = infoFor(e)
			}
		}

		seen := map[int64]bool{}
		var scans []store.ScanRecord
		var lastScan *int64
		pending := 0
		for _, e := range group {
			list, err := s.Store.ScansForIdentity(e.FTId, e.DisplayLogin())
			if err != nil {
				return nil, err
			}
			for _, r := range list {
				if seen[r.ID] {
					continue
				}
				seen[r.ID] = true
				scans = append(scans, r)
				if lastScan == nil || r.Timestamp > *lastScan {
					ts := r.Timestamp
					lastScan = &ts
				}
				if r.BlameStatus == store.BlameNotHandled {
					pending++
				}
			}
		}

		row := UserRow{
			Entry:        rep,
			Login:        rep.DisplayLogin(),
			ScanCount:    len(scans),
			PendingCount: pending,
			LastScan:     lastScan,
			HasError:     info == nil,
		}
		if info != nil {
			if info.Login != nil {
				row.Login = *info.Login
			}
			row.UserType = derefOr(info.UserType, "")
			row.CoalitionName = derefOr(info.CoalitionName, "")
			row.CoalitionColor = derefOr(info.CoalitionColor, "")
			row.PhotoURL = derefOr(info.PhotoURL, "")
			row.HasError = info.ProfileError || info.CoalitionError
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// AvailableCoalitions lists distinct coalition names present among the users.
func (s *Service) AvailableCoalitions() ([]string, error) {
	rows, err := s.AllUserRows()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		if r.CoalitionName != "" && !seen[r.CoalitionName] {
			seen[r.CoalitionName] = true
			out = append(out, r.CoalitionName)
		}
	}
	sort.Strings(out)
	return out, nil
}

// FilterUserRows ports ScanViewModel.userRows: search + filters + ordering.
func FilterUserRows(rows []UserRow, f UserFilters) []UserRow {
	q := strings.ToLower(strings.TrimSpace(f.Query))
	var out []UserRow
	for _, r := range rows {
		if q != "" && !strings.Contains(strings.ToLower(r.Login), q) && !strings.Contains(strings.ToLower(r.Entry.FullName), q) {
			continue
		}
		if f.Type != "" && r.UserType != f.Type {
			continue
		}
		if f.ScannedOnly && r.ScanCount == 0 {
			continue
		}
		if f.ErrorOnly && !r.HasError {
			continue
		}
		if f.Coalition != "" && r.CoalitionName != f.Coalition {
			continue
		}
		out = append(out, r)
	}

	nameOf := func(r UserRow) string {
		if r.Login != "" {
			return strings.ToLower(r.Login)
		}
		return strings.ToLower(r.Entry.FullName)
	}
	switch f.Order {
	case "alphabetical":
		sort.SliceStable(out, func(i, j int) bool { return nameOf(out[i]) < nameOf(out[j]) })
	case "blames":
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].ScanCount != out[j].ScanCount {
				return out[i].ScanCount > out[j].ScanCount
			}
			return nameOf(out[i]) < nameOf(out[j])
		})
	default: // "latest"
		sort.SliceStable(out, func(i, j int) bool {
			a, b := out[i].LastScan, out[j].LastScan
			if a == nil && b == nil {
				return nameOf(out[i]) < nameOf(out[j])
			}
			if a == nil {
				return false
			}
			if b == nil {
				return true
			}
			if *a != *b {
				return *a > *b
			}
			return nameOf(out[i]) < nameOf(out[j])
		})
	}
	return out
}

// UserDetail mirrors ScanViewModel.UserDetail.
type UserDetail struct {
	Entry             store.CADirEntry   `json:"entry"`
	Login             string             `json:"login"`
	FTId              string             `json:"ftId"`
	PhotoURL          string             `json:"photoUrl"`
	UserType          string             `json:"userType"`
	CoalitionName     string             `json:"coalitionName"`
	CoalitionColor    string             `json:"coalitionColor"`
	CoalitionImageURL string             `json:"coalitionImageUrl"`
	CoalitionID       *int               `json:"coalitionId,omitempty"`
	CoalitionsUserID  *int               `json:"coalitionsUserId,omitempty"`
	Location          string             `json:"location"`
	Level             *float64           `json:"level,omitempty"`
	CurrentProjects   []string           `json:"currentProjects"`
	Scans             []store.ScanRecord `json:"scans"`
}

// GetUserDetail finds the CA entry by pk and enriches it with cache + scans
// — port of ScanViewModel.openUser (without the "silently refresh in
// background" step, which the API exposes as an explicit refresh action).
func (s *Service) GetUserDetail(pk int64) (UserDetail, bool, error) {
	entries, err := s.Store.ListCADirectory()
	if err != nil {
		return UserDetail{}, false, err
	}
	var entry *store.CADirEntry
	for i := range entries {
		if entries[i].PK == pk {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		return UserDetail{}, false, nil
	}

	scans, err := s.Store.ScansForIdentity(entry.FTId, entry.DisplayLogin())
	if err != nil {
		return UserDetail{}, false, err
	}

	detail := UserDetail{Entry: *entry, Login: entry.DisplayLogin(), FTId: entry.FTId, Scans: scans}
	if key := entry.IntraKey(); key != "" {
		if info, ok, err := s.Store.PeekIntra(key); err == nil && ok {
			if info.Login != nil {
				detail.Login = *info.Login
			}
			if info.FTId != nil {
				detail.FTId = *info.FTId
			}
			detail.PhotoURL = derefOr(info.PhotoURL, "")
			detail.UserType = derefOr(info.UserType, "")
			detail.CoalitionName = derefOr(info.CoalitionName, "")
			detail.CoalitionColor = derefOr(info.CoalitionColor, "")
			detail.CoalitionImageURL = derefOr(info.CoalitionImageURL, "")
			detail.CoalitionID = info.CoalitionID
			detail.CoalitionsUserID = info.CoalitionsUserID
			detail.Location = derefOr(info.Location, "")
			detail.Level = info.Level
			detail.CurrentProjects = info.CurrentProjects
		}
	}
	return detail, true, nil
}

// DeleteUser removes a CA entry from the local cache only (never touches
// the CA itself), along with every blame recorded for them — mirrors
// ScanViewModel.deleteUser.
func (s *Service) DeleteUser(pk int64) error {
	entries, err := s.Store.ListCADirectory()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.PK == pk {
			if err := s.Store.DeleteScansForCADirEntry(pk, e.FTId, e.DisplayLogin()); err != nil {
				return err
			}
			break
		}
	}
	return s.Store.RemoveCADirectoryEntry(pk)
}

// RefreshUserProfile refetches just the 42 profile for one CA entry —
// mirrors ScanViewModel.refetchCurrentUserProfile.
func (s *Service) RefreshUserProfile(pk int64) error {
	settings, err := s.Store.GetSettings()
	if err != nil {
		return err
	}
	entry, err := s.findEntry(pk)
	if err != nil || entry == nil {
		return err
	}
	return s.refreshProfileForEntry(s.intraConfig(settings), *entry)
}

// refreshProfileForEntry is RefreshUserProfile's per-entry core, shared
// with RefreshAllIntraUsers (directory.go) so the bulk admin refresh
// doesn't duplicate this logic.
func (s *Service) refreshProfileForEntry(cfg intraclient.Config, entry store.CADirEntry) error {
	key := entry.IntraKey()
	if key == "" {
		return nil
	}
	cached, _, _ := s.Store.PeekIntra(key)
	user, err := s.Intra.FetchUser(cfg, entry.FTId, entry.DisplayLogin())
	if err != nil {
		cached.ProfileError = true
		return s.Store.PutIntra(key, cached)
	}
	fresh := store.IntraInfo{
		FetchedAt:         nowMillis(),
		Login:             strPtrOrNil(user.Login),
		FTId:              strPtrOrNil(user.ID),
		PhotoURL:          strPtrOrNil(user.ImageURL),
		UserType:          strPtrOrNil(userTypeFromCursus(user.CursusIDs)),
		Location:          strPtrOrNil(user.Location),
		Level:             user.Level,
		CurrentProjects:   user.CurrentProjects,
		CoalitionName:     cached.CoalitionName,
		CoalitionColor:    cached.CoalitionColor,
		CoalitionImageURL: cached.CoalitionImageURL,
		CoalitionID:       cached.CoalitionID,
		CoalitionsUserID:  cached.CoalitionsUserID,
		CoalitionError:    cached.CoalitionError,
	}
	return s.Store.PutIntra(key, fresh)
}

// RefreshUserCoalition refetches just the coalition for one CA entry —
// mirrors ScanViewModel.refetchCurrentUserCoalition.
func (s *Service) RefreshUserCoalition(pk int64) error {
	settings, err := s.Store.GetSettings()
	if err != nil {
		return err
	}
	entry, err := s.findEntry(pk)
	if err != nil || entry == nil {
		return err
	}
	return s.refreshCoalitionForEntry(s.intraConfig(settings), *entry)
}

// refreshCoalitionForEntry is RefreshUserCoalition's per-entry core, shared
// with RefreshAllCoalitions (directory.go).
func (s *Service) refreshCoalitionForEntry(cfg intraclient.Config, entry store.CADirEntry) error {
	key := entry.IntraKey()
	if key == "" {
		return nil
	}
	cached, _, _ := s.Store.PeekIntra(key)
	login := entry.DisplayLogin()
	if cached.Login != nil {
		login = *cached.Login
	}
	if login == "" {
		return nil
	}
	coalitions, err := s.Intra.FetchCoalitions(cfg, login)
	if err != nil {
		cached.CoalitionError = true
		return s.Store.PutIntra(key, cached)
	}
	picked := pickCoalition(coalitions)
	cached.FetchedAt = nowMillis()
	cached.CoalitionError = false
	if picked == nil {
		cached.CoalitionName, cached.CoalitionColor, cached.CoalitionImageURL, cached.CoalitionID = nil, nil, nil, nil
	} else {
		cached.CoalitionName = strPtrOrNil(picked.Name)
		cached.CoalitionColor = strPtrOrNil(picked.Color)
		cached.CoalitionImageURL = strPtrOrNil(picked.ImageURL)
		id := picked.ID
		cached.CoalitionID = &id
		if cus, err := s.Intra.FetchCoalitionsUsers(cfg, login); err == nil {
			for _, cu := range cus {
				if cu.CoalitionID == picked.ID {
					cuid := cu.ID
					cached.CoalitionsUserID = &cuid
					break
				}
			}
		}
	}
	return s.Store.PutIntra(key, cached)
}

func (s *Service) findEntry(pk int64) (*store.CADirEntry, error) {
	entries, err := s.Store.ListCADirectory()
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].PK == pk {
			return &entries[i], nil
		}
	}
	return nil, nil
}

// AddManualBlame records a scan as if the badge had just been tapped, using
// already-known info — mirrors ScanViewModel.addManualBlame.
func (s *Service) AddManualBlame(pk int64) (store.ScanRecord, error) {
	detail, ok, err := s.GetUserDetail(pk)
	if err != nil {
		return store.ScanRecord{}, err
	}
	if !ok {
		return store.ScanRecord{}, store.ErrNotFound
	}
	now := nowMillis()
	record := store.ScanRecord{
		Timestamp: now,
		UIDHex:    "manual:" + strconv.FormatInt(pk, 10) + ":" + strconv.FormatInt(now, 10),
		Wiegand:   "MANUAL",
		Login:     strPtrOrNil(detail.Login),
		FTId:      strPtrOrNil(detail.FTId),
		PhotoURL:  strPtrOrNil(detail.PhotoURL),
		UserType:  strPtrOrNil(detail.UserType),

		CoalitionName:     strPtrOrNil(detail.CoalitionName),
		CoalitionColor:    strPtrOrNil(detail.CoalitionColor),
		CoalitionImageURL: strPtrOrNil(detail.CoalitionImageURL),
		IsBlame:           true,
	}
	return s.Store.AddScanRecord(record)
}

// AssociateBadge links an unrecognized badge to a login and re-resolves the
// record — mirrors ScanViewModel.associateBadge.
func (s *Service) AssociateBadge(uidHex, login string) (store.ScanRecord, error) {
	login = strings.ToLower(strings.TrimSpace(login))
	if login == "" {
		return store.ScanRecord{}, nil
	}
	if err := s.Store.PutManualLink(uidHex, login); err != nil {
		return store.ScanRecord{}, err
	}
	outcome, err := s.Scan(uidHex)
	if err != nil {
		return store.ScanRecord{}, err
	}
	return outcome.Record, nil
}
