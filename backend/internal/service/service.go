// Package service is the server-side port of
// mobile/app/.../scan/ScanViewModel.kt: the scan flow, CA directory refresh,
// user aggregation, coalition/TIG actions and cluster caching, now backed
// by the shared SQLite store instead of per-device state.
package service

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"badgescanner/backend/internal/caclient"
	"badgescanner/backend/internal/intraclient"
	"badgescanner/backend/internal/logins"
	"badgescanner/backend/internal/store"
	"badgescanner/backend/internal/wiegand"
)

type Service struct {
	Store  *store.Store
	CA     *caclient.Client
	Intra  *intraclient.Client
	Events EventSink // nil until SetEvents is called; every use site checks for nil
}

func New(st *store.Store, ca *caclient.Client, intra *intraclient.Client) *Service {
	return &Service{Store: st, CA: ca, Intra: intra}
}

// SetEvents wires a live-event sink after construction — main.go builds the
// wshub.Hub and the Service separately (the hub doesn't need the service,
// only the reverse), then connects them with this call.
func (s *Service) SetEvents(e EventSink) {
	s.Events = e
}

func (s *Service) caConfig(a store.AppSettings) caclient.Config {
	return caclient.Config{Endpoint: a.CAEndpoint, Username: a.CAUsername, Password: a.CAPassword}
}

func (s *Service) intraConfig(a store.AppSettings) intraclient.Config {
	return intraclient.Config{TokenURL: a.FTTokenURL, Endpoint: a.FTEndpoint, UID: a.FTUid, Secret: a.FTSecret}
}

// ScanOutcome tells the frontend how to react — mirrors the mobile app's
// ScanState: a matched CA user opens the user page directly (no result
// card), an unmatched-but-resolved login shows the plain result card, and
// anything else is a failure the operator has to handle (e.g. Associate).
type ScanOutcome struct {
	Status string            `json:"status"` // "user" | "success" | "failure"
	Record store.ScanRecord  `json:"record"`
	Entry  *store.CADirEntry `json:"entry,omitempty"`
}

// resolvedBadge is the shared core result of resolving a badge UID to an
// identity: Wiegand → CA directory cache lookup → manual link fallback →
// intra cache/live resolve. No side effects (no history write) — that's
// deliberately left to callers, since Scan and Lookup want different
// side-effect/response shapes from the same resolution logic.
type resolvedBadge struct {
	codes                                            wiegand.Codes
	login, ftID, photoURL, matchedBadgeID, userType  string
	coalitionName, coalitionColor, coalitionImageURL string
	matchedEntry                                     *store.CADirEntry
	scanErr                                          string
}

// resolveBadge is the server-side port of ScanViewModel.process()'s
// resolution half (everything before the history write).
func (s *Service) resolveBadge(uidHex string) (resolvedBadge, error) {
	codes, err := wiegand.FromUIDHex(uidHex)
	if err != nil {
		return resolvedBadge{}, err
	}

	settings, err := s.Store.GetSettings()
	if err != nil {
		return resolvedBadge{}, err
	}

	rb := resolvedBadge{codes: codes}

	if !settings.CAConfigured() {
		rb.scanErr = "CA credentials are not configured (see Admin settings)"
	} else {
		entry, badgeNum, found, err := s.Store.FindByBadge(codes.CACandidates())
		if err != nil {
			return resolvedBadge{}, err
		}
		if !found {
			manualLogin, hasManual, err := s.Store.GetManualLink(codes.UIDHex)
			if err != nil {
				return resolvedBadge{}, err
			}
			if hasManual {
				rb.login = manualLogin
				entries, _ := s.Store.ListCADirectory()
				for _, e := range entries {
					if strings.EqualFold(e.DisplayLogin(), manualLogin) {
						ec := e
						rb.matchedEntry = &ec
						break
					}
				}
			} else {
				dirEntries, _ := s.Store.ListCADirectory()
				rb.scanErr = fmt.Sprintf(
					"Badge not in CA directory (tried %s; %d users cached). "+
						"If this badge is new, use “Refetch CA users” in Admin, or associate it to a student.",
					strings.Join(codes.CACandidates(), ", "), len(dirEntries))
			}
		} else {
			rb.matchedBadgeID = strconv.FormatInt(badgeNum, 10)
			ec := entry
			rb.matchedEntry = &ec
			rb.login = entry.FTLogin
			rb.ftID = entry.FTId
			if rb.login == "" && rb.ftID == "" {
				rb.login = logins.PiscineLoginFromName(entry.FullName)
			}
			if rb.login == "" && rb.ftID == "" {
				name := entry.FullName
				if name == "" {
					name = strconv.FormatInt(entry.PK, 10)
				}
				rb.scanErr = fmt.Sprintf("CA user %s has no ft_login and no ft_id", name)
			}
		}
	}

	if rb.scanErr == "" {
		if !settings.FTConfigured() {
			if rb.login == "" {
				rb.scanErr = "42 API credentials are not configured (see Admin settings)"
			}
		} else if rb.matchedEntry != nil {
			cacheKey := rb.ftID
			if cacheKey == "" {
				cacheKey = rb.login
			}
			if cacheKey != "" {
				if cached, ok, err := s.Store.PeekIntra(cacheKey); err == nil && ok {
					if cached.Login != nil {
						rb.login = *cached.Login
					}
					if cached.FTId != nil {
						rb.ftID = *cached.FTId
					}
					rb.photoURL = derefOr(cached.PhotoURL, "")
					rb.userType = derefOr(cached.UserType, "")
					rb.coalitionName = derefOr(cached.CoalitionName, "")
					rb.coalitionColor = derefOr(cached.CoalitionColor, "")
					rb.coalitionImageURL = derefOr(cached.CoalitionImageURL, "")
				}
			}
		} else {
			info, err := s.resolveIntra(settings, rb.login, rb.ftID)
			if err != nil {
				if rb.login == "" {
					rb.scanErr = err.Error()
				}
			} else if info != nil {
				if info.Login != nil {
					rb.login = *info.Login
				}
				if info.FTId != nil {
					rb.ftID = *info.FTId
				}
				rb.photoURL = derefOr(info.PhotoURL, "")
				rb.userType = derefOr(info.UserType, "")
				rb.coalitionName = derefOr(info.CoalitionName, "")
				rb.coalitionColor = derefOr(info.CoalitionColor, "")
				rb.coalitionImageURL = derefOr(info.CoalitionImageURL, "")
			}
		}
	}

	return rb, nil
}

// Scan is the server-side port of ScanViewModel.process(): resolves the
// badge via resolveBadge, then writes a scan_history record (which is
// what carries the blame/TIG fields — see store.ScanRecord).
func (s *Service) Scan(uidHex string) (ScanOutcome, error) {
	rb, err := s.resolveBadge(uidHex)
	if err != nil {
		return ScanOutcome{}, err
	}

	wiegandVal := rb.codes.Wiegand26
	if rb.matchedBadgeID != "" {
		wiegandVal = rb.matchedBadgeID
	}
	record := store.ScanRecord{
		Timestamp: time.Now().UnixMilli(),
		UIDHex:    rb.codes.UIDHex,
		MifareHex: rb.codes.MifareHex,
		Wiegand:   wiegandVal,
		Login:     strPtrOrNil(rb.login),
		FTId:      strPtrOrNil(rb.ftID),
		PhotoURL:  strPtrOrNil(rb.photoURL),
		Error:     strPtrOrNil(rb.scanErr),
		UserType:  strPtrOrNil(rb.userType),

		CoalitionName:     strPtrOrNil(rb.coalitionName),
		CoalitionColor:    strPtrOrNil(rb.coalitionColor),
		CoalitionImageURL: strPtrOrNil(rb.coalitionImageURL),
	}
	saved, err := s.Store.AddScanRecord(record)
	if err != nil {
		return ScanOutcome{}, err
	}

	switch {
	case rb.scanErr == "" && rb.login != "" && rb.matchedEntry != nil:
		return ScanOutcome{Status: "user", Record: saved, Entry: rb.matchedEntry}, nil
	case rb.scanErr == "" && rb.login != "":
		return ScanOutcome{Status: "success", Record: saved}, nil
	default:
		return ScanOutcome{Status: "failure", Record: saved}, nil
	}
}

// LookupResult is the deliberately narrow shape returned to scoped
// "lookup"-permission API keys (the C client): login + coalition theming +
// photo only. No ftId, level, currentProjects, location, coalition ids, CA
// entry pk, or anything scan_history/blame/TIG-shaped — those fields
// simply have no field to land in here.
type LookupResult struct {
	Found             bool   `json:"found"`
	Login             string `json:"login,omitempty"`
	CoalitionName     string `json:"coalitionName,omitempty"`
	CoalitionColor    string `json:"coalitionColor,omitempty"`
	CoalitionImageURL string `json:"coalitionImageUrl,omitempty"`
	PhotoURL          string `json:"photoUrl,omitempty"`
}

// Lookup is the restricted counterpart to Scan for the C badge-lookup
// client: same resolution (resolveBadge), but writes nothing to
// scan_history and returns only LookupResult's allowlisted fields. A
// "lookup"-scope API key can only ever reach this method (see api.go's
// route wiring — /api/lookup is the sole route behind that scope), so it's
// structurally unable to see blame/TIG/points data, not just asked nicely
// not to: that data lives solely in store.ScanRecord, which only Scan
// writes to and which Lookup never touches.
func (s *Service) Lookup(uidHex string) (LookupResult, error) {
	rb, err := s.resolveBadge(uidHex)
	if err != nil {
		return LookupResult{}, err
	}
	if rb.scanErr != "" || rb.login == "" {
		return LookupResult{Found: false}, nil
	}
	return LookupResult{
		Found:             true,
		Login:             rb.login,
		CoalitionName:     rb.coalitionName,
		CoalitionColor:    rb.coalitionColor,
		CoalitionImageURL: rb.coalitionImageURL,
		PhotoURL:          rb.photoURL,
	}, nil
}

// resolveIntra mirrors ScanViewModel.resolveIntra: 12h-cache-or-fetch.
func (s *Service) resolveIntra(settings store.AppSettings, login, ftID string) (*store.IntraInfo, error) {
	cacheKey := ftID
	if cacheKey == "" {
		cacheKey = login
	}
	if cacheKey == "" {
		return nil, nil
	}
	if fresh, ok, err := s.Store.GetFreshIntra(cacheKey); err == nil && ok {
		return &fresh, nil
	}
	info, err := s.fetchIntraInfo(settings, login, ftID)
	if err != nil {
		return nil, err
	}
	if err := s.Store.PutIntra(cacheKey, info); err != nil {
		return nil, err
	}
	return &info, nil
}

// fetchIntraInfo mirrors ScanViewModel.fetchIntraInfo: profile + best-effort coalition.
func (s *Service) fetchIntraInfo(settings store.AppSettings, login, ftID string) (store.IntraInfo, error) {
	cfg := s.intraConfig(settings)
	user, err := s.Intra.FetchUser(cfg, ftID, login)
	if err != nil {
		return store.IntraInfo{}, err
	}
	userType := userTypeFromCursus(user.CursusIDs)
	resolvedLogin := user.Login
	if resolvedLogin == "" {
		resolvedLogin = login
	}
	resolvedFTId := user.ID
	if resolvedFTId == "" {
		resolvedFTId = ftID
	}
	info := store.IntraInfo{
		FetchedAt:       time.Now().UnixMilli(),
		Login:           strPtrOrNil(resolvedLogin),
		FTId:            strPtrOrNil(resolvedFTId),
		PhotoURL:        strPtrOrNil(user.ImageURL),
		UserType:        strPtrOrNil(userType),
		Location:        strPtrOrNil(user.Location),
		Level:           user.Level,
		CurrentProjects: user.CurrentProjects,
	}
	if resolvedLogin != "" {
		coalitions, err := s.Intra.FetchCoalitions(cfg, resolvedLogin)
		if err == nil {
			if picked := pickCoalition(coalitions); picked != nil {
				info.CoalitionName = strPtrOrNil(picked.Name)
				info.CoalitionColor = strPtrOrNil(picked.Color)
				info.CoalitionImageURL = strPtrOrNil(picked.ImageURL)
				id := picked.ID
				info.CoalitionID = &id
				if cus, err := s.Intra.FetchCoalitionsUsers(cfg, resolvedLogin); err == nil {
					for _, cu := range cus {
						if cu.CoalitionID == picked.ID {
							cuid := cu.ID
							info.CoalitionsUserID = &cuid
							break
						}
					}
				}
			}
		}
	}
	return info, nil
}

// Priority order for theming/display: main coalitions win over piscine
// ones; otherwise the highest-scoring coalition — ports
// ScanViewModel.pickCoalition.
var priorityCoalitions = []string{"harkonnen", "corrino", "atreides"}
var secondaryCoalitions = []string{"hordes", "alliance"}

func pickCoalition(coalitions []intraclient.Coalition) *intraclient.Coalition {
	match := func(keys []string) *intraclient.Coalition {
		for _, key := range keys {
			for i := range coalitions {
				c := coalitions[i]
				if strings.EqualFold(c.Slug, key) || strings.EqualFold(c.Name, key) {
					return &c
				}
			}
		}
		return nil
	}
	if c := match(priorityCoalitions); c != nil {
		return c
	}
	if c := match(secondaryCoalitions); c != nil {
		return c
	}
	if len(coalitions) == 0 {
		return nil
	}
	best := coalitions[0]
	for _, c := range coalitions[1:] {
		if c.Score > best.Score {
			best = c
		}
	}
	return &best
}

// userTypeFromCursus ports ScanViewModel.userTypeFromCursus: cursus 21 =>
// main student cursus, cursus 9 => piscine.
func userTypeFromCursus(cursusIDs []int) string {
	for _, id := range cursusIDs {
		if id == 21 {
			return "Student"
		}
	}
	for _, id := range cursusIDs {
		if id == 9 {
			return "Piscine"
		}
	}
	return ""
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefOr(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}

func nowMillis() int64 {
	return time.Now().UnixMilli()
}
