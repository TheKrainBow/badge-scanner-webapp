package service

import (
	"fmt"

	"badgescanner/backend/internal/store"
)

// GiveCoalitionPoints ports ScanViewModel.giveCoalitionPoints: resolves
// coalitions_user_id lazily (cached from then on) and posts the score.
func (s *Service) GiveCoalitionPoints(pk int64, value int, reason string) (string, error) {
	settings, err := s.Store.GetSettings()
	if err != nil {
		return "", err
	}
	detail, ok, err := s.GetUserDetail(pk)
	if err != nil {
		return "", err
	}
	if !ok || detail.CoalitionID == nil || detail.Login == "" {
		return "", fmt.Errorf("coalition inconnue pour cet utilisateur")
	}
	cfg := s.intraConfig(settings)

	coalitionsUserID := detail.CoalitionsUserID
	if coalitionsUserID == nil {
		cus, err := s.Intra.FetchCoalitionsUsers(cfg, detail.Login)
		if err == nil {
			for _, cu := range cus {
				if cu.CoalitionID == *detail.CoalitionID {
					id := cu.ID
					coalitionsUserID = &id
					break
				}
			}
		}
		if coalitionsUserID != nil {
			if key := detail.Entry.IntraKey(); key != "" {
				if cached, ok, _ := s.Store.PeekIntra(key); ok {
					cached.CoalitionsUserID = coalitionsUserID
					_ = s.Store.PutIntra(key, cached)
				}
			}
		}
	}
	if coalitionsUserID == nil {
		return "", fmt.Errorf("coalition inconnue pour cet utilisateur")
	}

	if err := s.Intra.PostCoalitionScore(cfg, *detail.CoalitionID, *coalitionsUserID, value, reason); err != nil {
		return "", fmt.Errorf("échec de l'envoi des points: %w", err)
	}
	return fmt.Sprintf("%d point(s) envoyé(s) à %s", value, detail.Login), nil
}

// GiveTig ports ScanViewModel.giveTig.
func (s *Service) GiveTig(pk int64, durationSeconds int, reason string) (string, error) {
	settings, err := s.Store.GetSettings()
	if err != nil {
		return "", err
	}
	if settings.CloserID == "" {
		return "", fmt.Errorf("closer ID non configuré (voir Admin) ou utilisateur 42 inconnu")
	}
	detail, ok, err := s.GetUserDetail(pk)
	if err != nil {
		return "", err
	}
	if !ok || detail.FTId == "" {
		return "", fmt.Errorf("closer ID non configuré (voir Admin) ou utilisateur 42 inconnu")
	}
	cfg := s.intraConfig(settings)
	if err := s.Intra.PostTig(cfg, detail.FTId, settings.CloserID, reason, durationSeconds); err != nil {
		return "", fmt.Errorf("échec de l'envoi du TIG: %w", err)
	}
	hours := durationSeconds / 3600
	who := detail.Login
	if who == "" {
		who = detail.FTId
	}
	return fmt.Sprintf("TIG de %dh envoyé à %s", hours, who), nil
}

// UpdateScan patches a history record's reason/blame status — mirrors
// ScanViewModel.setScanReason/setBlameStatus.
func (s *Service) UpdateScan(id int64, reason *string, blameStatus *store.BlameStatus, tigDuration *string) error {
	return s.Store.UpdateScanRecord(id, reason, blameStatus, tigDuration)
}
