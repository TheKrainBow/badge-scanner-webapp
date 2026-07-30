package service

import (
	"regexp"
	"sort"
	"strconv"
	"sync"

	"badgescanner/backend/internal/clustersvg"
	"badgescanner/backend/internal/intraclient"
)

// ClusterData is the combined clusters + parsed layouts + who's-sitting-where
// payload, cached per campus id — mirrors ScanViewModel's
// clusters/clusterLayouts/clusterOccupants + clustersLoadedForCampus guard.
type ClusterData struct {
	Clusters  []intraclient.ClusterInfo  `json:"clusters"`
	Layouts   map[int]clustersvg.Layout  `json:"layouts"`
	Occupants map[string]ClusterOccupant `json:"occupants"`
}

type ClusterOccupant struct {
	Login    string `json:"login"`
	PhotoURL string `json:"photoUrl"`
}

var clusterNumberRegex = regexp.MustCompile(`\d+`)

type clusterCacheEntry struct {
	campusID string
	data     ClusterData
}

var clusterMu sync.Mutex
var clusterCache clusterCacheEntry

// LoadClusters fetches clusters + seat-map SVGs (parsed once) for a campus,
// unless already cached for that campus id and force is false — mirrors
// ScanViewModel.loadClusters's clustersLoadedForCampus guard.
func (s *Service) LoadClusters(campusID string, force bool) (ClusterData, error) {
	clusterMu.Lock()
	if !force && clusterCache.campusID == campusID && len(clusterCache.data.Clusters) > 0 {
		cached := clusterCache.data
		clusterMu.Unlock()
		occupants, err := s.fetchOccupants(campusID)
		if err != nil {
			return cached, err
		}
		cached.Occupants = occupants
		clusterMu.Lock()
		clusterCache.data.Occupants = occupants
		clusterMu.Unlock()
		return cached, nil
	}
	clusterMu.Unlock()

	settings, err := s.Store.GetSettings()
	if err != nil {
		return ClusterData{}, err
	}
	cfg := s.intraConfig(settings)

	list, err := s.Intra.FetchClusters(cfg, campusID)
	if err != nil {
		return ClusterData{}, err
	}
	sort.SliceStable(list, func(i, j int) bool {
		return numberIn(list[i].Name) < numberIn(list[j].Name)
	})

	layouts := map[int]clustersvg.Layout{}
	for _, c := range list {
		svg, err := s.Intra.FetchClusterSvg(c.CDNLink)
		if err != nil {
			return ClusterData{}, err
		}
		layouts[c.ID] = clustersvg.Parse(svg)
	}

	occupants, err := s.fetchOccupants(campusID)
	if err != nil {
		return ClusterData{}, err
	}

	data := ClusterData{Clusters: list, Layouts: layouts, Occupants: occupants}
	clusterMu.Lock()
	clusterCache = clusterCacheEntry{campusID: campusID, data: data}
	clusterMu.Unlock()
	return data, nil
}

// RefreshClusterOccupants refreshes just who's sitting where.
func (s *Service) RefreshClusterOccupants(campusID string) (map[string]ClusterOccupant, error) {
	return s.fetchOccupants(campusID)
}

func (s *Service) fetchOccupants(campusID string) (map[string]ClusterOccupant, error) {
	settings, err := s.Store.GetSettings()
	if err != nil {
		return nil, err
	}
	locations, err := s.Intra.FetchActiveLocations(s.intraConfig(settings), campusID)
	if err != nil {
		return nil, err
	}
	out := map[string]ClusterOccupant{}
	for _, l := range locations {
		out[l.Host] = ClusterOccupant{Login: l.Login, PhotoURL: l.PhotoURL}
	}
	return out, nil
}

func numberIn(name string) int {
	m := clusterNumberRegex.FindString(name)
	if m == "" {
		return int(^uint(0) >> 1) // math.MaxInt
	}
	n, err := strconv.Atoi(m)
	if err != nil {
		return int(^uint(0) >> 1)
	}
	return n
}
