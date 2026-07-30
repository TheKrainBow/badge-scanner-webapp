package store

import (
	"database/sql"
)

// BlameStatus mirrors mobile/app/.../data/HistoryStore.kt's BlameStatus.
type BlameStatus string

const (
	BlameNotHandled BlameStatus = "NOT_HANDLED"
	BlamePardoned   BlameStatus = "PARDONED"
	BlameTiged      BlameStatus = "TIGED"
)

// ScanRecord mirrors mobile/app/.../data/HistoryStore.kt's ScanRecord.
type ScanRecord struct {
	ID                int64       `json:"id"`
	Timestamp         int64       `json:"timestamp"`
	UIDHex            string      `json:"uidHex"`
	MifareHex         string      `json:"mifareHex"`
	Wiegand           string      `json:"wiegand"`
	Login             *string     `json:"login,omitempty"`
	FTId              *string     `json:"ftId,omitempty"`
	PhotoURL          *string     `json:"photoUrl,omitempty"`
	Error             *string     `json:"error,omitempty"`
	UserType          *string     `json:"userType,omitempty"`
	CoalitionName     *string     `json:"coalitionName,omitempty"`
	CoalitionColor    *string     `json:"coalitionColor,omitempty"`
	CoalitionImageURL *string     `json:"coalitionImageUrl,omitempty"`
	Reason            *string     `json:"reason,omitempty"`
	IsBlame           bool        `json:"isBlame"`
	BlameStatus       BlameStatus `json:"blameStatus"`
	TigDuration       *string     `json:"tigDuration,omitempty"`
}

func (s *Store) AddScanRecord(r ScanRecord) (ScanRecord, error) {
	if r.BlameStatus == "" {
		r.BlameStatus = BlameNotHandled
	}
	res, err := s.DB.Exec(`INSERT INTO scan_history (timestamp, uid_hex, mifare_hex, wiegand, login, ft_id,
		photo_url, error, user_type, coalition_name, coalition_color, coalition_image_url, reason, is_blame,
		blame_status, tig_duration) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Timestamp, r.UIDHex, r.MifareHex, r.Wiegand, ptrToNull(r.Login), ptrToNull(r.FTId),
		ptrToNull(r.PhotoURL), ptrToNull(r.Error), ptrToNull(r.UserType), ptrToNull(r.CoalitionName),
		ptrToNull(r.CoalitionColor), ptrToNull(r.CoalitionImageURL), ptrToNull(r.Reason), r.IsBlame,
		string(r.BlameStatus), ptrToNull(r.TigDuration),
	)
	if err != nil {
		return ScanRecord{}, err
	}
	id, _ := res.LastInsertId()
	r.ID = id
	return r, nil
}

func (s *Store) ListScanHistory(limit, offset int) ([]ScanRecord, error) {
	rows, err := s.DB.Query(`SELECT id, timestamp, uid_hex, mifare_hex, wiegand, login, ft_id, photo_url,
		error, user_type, coalition_name, coalition_color, coalition_image_url, reason, is_blame,
		blame_status, tig_duration FROM scan_history ORDER BY timestamp DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecordRows(rows)
}

// ScansForIdentity returns blames (isBlame=true) matching an ft_id or login,
// mirroring ScanViewModel.scansFor.
func (s *Store) ScansForIdentity(ftID, login string) ([]ScanRecord, error) {
	rows, err := s.DB.Query(`SELECT id, timestamp, uid_hex, mifare_hex, wiegand, login, ft_id, photo_url,
		error, user_type, coalition_name, coalition_color, coalition_image_url, reason, is_blame,
		blame_status, tig_duration FROM scan_history
		WHERE is_blame = 1 AND ((? != '' AND ft_id = ?) OR (? != '' AND LOWER(login) = LOWER(?)))
		ORDER BY timestamp DESC`, ftID, ftID, login, login)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecordRows(rows)
}

func scanRecordRows(rows *sql.Rows) ([]ScanRecord, error) {
	var out []ScanRecord
	for rows.Next() {
		var r ScanRecord
		var login, ftID, photoURL, errStr, userType, coName, coColor, coImage, reason, tigDuration sql.NullString
		var isBlame int
		var blameStatus string
		if err := rows.Scan(&r.ID, &r.Timestamp, &r.UIDHex, &r.MifareHex, &r.Wiegand, &login, &ftID,
			&photoURL, &errStr, &userType, &coName, &coColor, &coImage, &reason, &isBlame,
			&blameStatus, &tigDuration); err != nil {
			return nil, err
		}
		r.Login = nullToPtr(login)
		r.FTId = nullToPtr(ftID)
		r.PhotoURL = nullToPtr(photoURL)
		r.Error = nullToPtr(errStr)
		r.UserType = nullToPtr(userType)
		r.CoalitionName = nullToPtr(coName)
		r.CoalitionColor = nullToPtr(coColor)
		r.CoalitionImageURL = nullToPtr(coImage)
		r.Reason = nullToPtr(reason)
		r.IsBlame = isBlame != 0
		r.BlameStatus = BlameStatus(blameStatus)
		r.TigDuration = nullToPtr(tigDuration)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetScanRecord(id int64) (ScanRecord, error) {
	rows, err := s.DB.Query(`SELECT id, timestamp, uid_hex, mifare_hex, wiegand, login, ft_id, photo_url,
		error, user_type, coalition_name, coalition_color, coalition_image_url, reason, is_blame,
		blame_status, tig_duration FROM scan_history WHERE id = ?`, id)
	if err != nil {
		return ScanRecord{}, err
	}
	defer rows.Close()
	list, err := scanRecordRows(rows)
	if err != nil {
		return ScanRecord{}, err
	}
	if len(list) == 0 {
		return ScanRecord{}, ErrNotFound
	}
	return list[0], nil
}

// UpdateScanRecord patches reason / blameStatus / tigDuration — the fields
// the frontend can edit (mirrors ScanViewModel.setScanReason/setBlameStatus).
func (s *Store) UpdateScanRecord(id int64, reason *string, blameStatus *BlameStatus, tigDuration *string) error {
	if reason != nil {
		if _, err := s.DB.Exec(`UPDATE scan_history SET reason = ? WHERE id = ?`, ptrToNull(reason), id); err != nil {
			return err
		}
	}
	if blameStatus != nil {
		var tig sql.NullString
		if *blameStatus == BlameTiged {
			tig = ptrToNull(tigDuration)
		}
		if _, err := s.DB.Exec(`UPDATE scan_history SET blame_status = ?, tig_duration = ? WHERE id = ?`,
			string(*blameStatus), tig, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteScanRecord(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM scan_history WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteScansForCADirEntry(pk int64, ftID, login string) error {
	_, err := s.DB.Exec(`DELETE FROM scan_history
		WHERE is_blame = 1 AND ((? != '' AND ft_id = ?) OR (? != '' AND LOWER(login) = LOWER(?)))`,
		ftID, ftID, login, login)
	return err
}

func (s *Store) ClearHistory() error {
	_, err := s.DB.Exec(`DELETE FROM scan_history`)
	return err
}
