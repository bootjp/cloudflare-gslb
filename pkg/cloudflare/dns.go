package cloudflare

import (
	"context"
	"time"

	cf "github.com/cloudflare/cloudflare-go"
	"github.com/cockroachdb/errors"
)

type DNSClientInterface interface {
	GetDNSRecords(ctx context.Context, name, recordType string) ([]cf.DNSRecord, error)
	DeleteDNSRecord(ctx context.Context, recordID string) error
	CreateDNSRecord(ctx context.Context, name, recordType, content string) (cf.DNSRecord, error)
	UpdateDNSRecord(ctx context.Context, recordID, name, recordType, content string) (cf.DNSRecord, error)
	ReplaceRecords(ctx context.Context, name, recordType, newContent string) error
	GetZoneID() string
}

type DNSClient struct {
	api      *cf.API
	zoneID   string
	proxied  bool
	ttl      int
	priority uint16
}

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

func (c *DNSClient) GetZoneID() string {
	return c.zoneID
}

func (c *DNSClient) GetDNSRecords(ctx context.Context, name, recordType string) ([]cf.DNSRecord, error) {
	params := cf.ListDNSRecordsParams{
		Name: name,
		Type: recordType,
	}

	records, _, err := c.api.ListDNSRecords(ctx, cf.ZoneIdentifier(c.zoneID), params)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return records, nil
}

func (c *DNSClient) DeleteDNSRecord(ctx context.Context, recordID string) error {
	err := c.api.DeleteDNSRecord(ctx, cf.ZoneIdentifier(c.zoneID), recordID)
	if err != nil {
		return errors.WithStack(err)
	}
	return nil
}

func (c *DNSClient) CreateDNSRecord(ctx context.Context, name, recordType, content string) (cf.DNSRecord, error) {
	params := cf.CreateDNSRecordParams{
		Type:     recordType,
		Name:     name,
		Content:  content,
		TTL:      c.ttl,
		Proxied:  &c.proxied,
		Priority: &c.priority,
	}

	record, err := c.api.CreateDNSRecord(ctx, cf.ZoneIdentifier(c.zoneID), params)
	if err != nil {
		return cf.DNSRecord{}, errors.WithStack(err)
	}

	return record, nil
}

func (c *DNSClient) UpdateDNSRecord(ctx context.Context, recordID, name, recordType, content string) (cf.DNSRecord, error) {
	params := cf.UpdateDNSRecordParams{
		ID:       recordID,
		Type:     recordType,
		Name:     name,
		Content:  content,
		TTL:      c.ttl,
		Proxied:  &c.proxied,
		Priority: &c.priority,
	}

	record, err := c.api.UpdateDNSRecord(ctx, cf.ZoneIdentifier(c.zoneID), params)
	if err != nil {
		return cf.DNSRecord{}, errors.WithStack(err)
	}

	return record, nil
}

func (c *DNSClient) ReplaceRecords(ctx context.Context, name, recordType, newContent string) error {
	records, err := c.GetDNSRecords(ctx, name, recordType)
	if err != nil {
		return err
	}

	for _, record := range records {
		if err := c.DeleteDNSRecord(ctx, record.ID); err != nil {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}

	_, err = c.CreateDNSRecord(ctx, name, recordType, newContent)
	if err != nil {
		return err
	}

	return nil
}
