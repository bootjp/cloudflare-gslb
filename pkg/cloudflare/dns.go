package cloudflare

import (
	"context"
	"time"

	cf "github.com/cloudflare/cloudflare-go"
	"github.com/cockroachdb/errors"
)

// DNSClientInterface はDNSの操作を行うインターフェース
type DNSClientInterface interface {
	GetDNSRecords(ctx context.Context, name, recordType string) ([]cf.DNSRecord, error)
	DeleteDNSRecord(ctx context.Context, recordID string) error
	CreateDNSRecord(ctx context.Context, name, recordType, content string) (cf.DNSRecord, error)
	UpdateDNSRecord(ctx context.Context, recordID, name, recordType, content string) (cf.DNSRecord, error)
	ReplaceRecords(ctx context.Context, name, recordType, newContent string) error
}

// DNSClient はCloudflare DNSの操作を行うクライアント
type DNSClient struct {
	api      *cf.API
	zoneID   string
	proxied  bool
	ttl      int
	priority uint16
}

// NewDNSClient はDNSClientを初期化する
func NewDNSClient(apiToken, zoneID string, proxied bool, ttl int) (*DNSClient, error) {
	api, err := cf.NewWithAPIToken(apiToken)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &DNSClient{
		api:     api,
		zoneID:  zoneID,
		proxied: proxied,
		ttl:     ttl,
	}, nil
}

// GetDNSRecords は指定された名前のDNSレコードを取得する
func (c *DNSClient) GetDNSRecords(ctx context.Context, name, recordType string) ([]cf.DNSRecord, error) {
	// レコードの検索パラメータ
	params := cf.ListDNSRecordsParams{
		Name: name,
		Type: recordType,
	}

	// レコードの取得
	records, _, err := c.api.ListDNSRecords(ctx, cf.ZoneIdentifier(c.zoneID), params)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return records, nil
}

// DeleteDNSRecord はDNSレコードを削除する
func (c *DNSClient) DeleteDNSRecord(ctx context.Context, recordID string) error {
	err := c.api.DeleteDNSRecord(ctx, cf.ZoneIdentifier(c.zoneID), recordID)
	if err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// CreateDNSRecord は新しいDNSレコードを作成する
func (c *DNSClient) CreateDNSRecord(ctx context.Context, name, recordType, content string) (cf.DNSRecord, error) {
	// レコード作成のパラメータ
	params := cf.CreateDNSRecordParams{
		Type:     recordType,
		Name:     name,
		Content:  content,
		TTL:      c.ttl,
		Proxied:  &c.proxied,
		Priority: &c.priority,
	}

	// レコードの作成
	record, err := c.api.CreateDNSRecord(ctx, cf.ZoneIdentifier(c.zoneID), params)
	if err != nil {
		return cf.DNSRecord{}, errors.WithStack(err)
	}

	return record, nil
}

// UpdateDNSRecord はDNSレコードを更新する
func (c *DNSClient) UpdateDNSRecord(ctx context.Context, recordID, name, recordType, content string) (cf.DNSRecord, error) {
	// レコード更新のパラメータ
	params := cf.UpdateDNSRecordParams{
		ID:       recordID,
		Type:     recordType,
		Name:     name,
		Content:  content,
		TTL:      c.ttl,
		Proxied:  &c.proxied,
		Priority: &c.priority,
	}

	// レコードの更新
	record, err := c.api.UpdateDNSRecord(ctx, cf.ZoneIdentifier(c.zoneID), params)
	if err != nil {
		return cf.DNSRecord{}, errors.WithStack(err)
	}

	return record, nil
}

// ReplaceRecords は既存のDNSレコードを削除して新しいレコードを作成する
func (c *DNSClient) ReplaceRecords(ctx context.Context, name, recordType, newContent string) error {
	// 既存のレコードを取得
	records, err := c.GetDNSRecords(ctx, name, recordType)
	if err != nil {
		return err
	}

	// 既存のレコードがある場合は削除
	for _, record := range records {
		if err := c.DeleteDNSRecord(ctx, record.ID); err != nil {
			return err
		}
		// 少し待機して削除が完了するのを待つ
		time.Sleep(500 * time.Millisecond)
	}

	// 新しいレコードを作成
	_, err = c.CreateDNSRecord(ctx, name, recordType, newContent)
	if err != nil {
		return err
	}

	return nil
}
