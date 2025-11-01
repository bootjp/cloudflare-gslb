package cloudflare

import (
	"context"
	"time"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/dns"
	"github.com/cloudflare/cloudflare-go/v6/option"
	"github.com/cloudflare/cloudflare-go/v6/packages/pagination"
	"github.com/cockroachdb/errors"
)

type cloudflareAPI interface {
	New(ctx context.Context, params dns.RecordNewParams, opts ...option.RequestOption) (*dns.RecordResponse, error)
	Delete(ctx context.Context, dnsRecordID string, body dns.RecordDeleteParams, opts ...option.RequestOption) (*dns.RecordDeleteResponse, error)
	List(ctx context.Context, params dns.RecordListParams, opts ...option.RequestOption) (*pagination.V4PagePaginationArray[dns.RecordResponse], error)
	Update(ctx context.Context, dnsRecordID string, params dns.RecordUpdateParams, opts ...option.RequestOption) (*dns.RecordResponse, error)
}

type DNSClientInterface interface {
	GetDNSRecords(ctx context.Context, name, recordType string) ([]dns.RecordResponse, error)
	DeleteDNSRecord(ctx context.Context, recordID string) error
	CreateDNSRecord(ctx context.Context, name, recordType, content string) (dns.RecordResponse, error)
	UpdateDNSRecord(ctx context.Context, recordID, name, recordType, content string) (dns.RecordResponse, error)
	ReplaceRecords(ctx context.Context, name, recordType, newContent string) error
	GetZoneID() string
}

type DNSClient struct {
	api      cloudflareAPI
	zoneID   string
	proxied  bool
	ttl      int
	priority uint16
}

func NewDNSClient(apiToken, zoneID string, proxied bool, ttl int) (*DNSClient, error) {
	client := cf.NewClient(
		option.WithAPIToken(apiToken),
	)

	return &DNSClient{
		api:     client.DNS.Records,
		zoneID:  zoneID,
		proxied: proxied,
		ttl:     ttl,
	}, nil
}

func (c *DNSClient) GetZoneID() string {
	return c.zoneID
}

func (c *DNSClient) GetDNSRecords(ctx context.Context, name, recordType string) ([]dns.RecordResponse, error) {
	params := dns.RecordListParams{
		ZoneID: cf.F(c.zoneID),
		Name: cf.F(dns.RecordListParamsName{
			Exact: cf.F(name),
		}),
		Type: cf.F(dns.RecordListParamsType(recordType)),
	}

	result, err := c.api.List(ctx, params)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return result.Result, nil
}

func (c *DNSClient) DeleteDNSRecord(ctx context.Context, recordID string) error {
	_, err := c.api.Delete(ctx, recordID, dns.RecordDeleteParams{
		ZoneID: cf.F(c.zoneID),
	})
	if err != nil {
		return errors.WithStack(err)
	}
	return nil
}

func (c *DNSClient) CreateDNSRecord(ctx context.Context, name, recordType, content string) (dns.RecordResponse, error) {
	// Build the record data based on type
	var body dns.RecordNewParamsBodyUnion
	switch recordType {
	case "A":
		body = dns.ARecordParam{
			Type:    cf.F(dns.ARecordTypeA),
			Name:    cf.F(name),
			Content: cf.F(content),
			TTL:     cf.F(dns.TTL(c.ttl)),
			Proxied: cf.F(c.proxied),
		}
	case "AAAA":
		body = dns.AAAARecordParam{
			Type:    cf.F(dns.AAAARecordTypeAAAA),
			Name:    cf.F(name),
			Content: cf.F(content),
			TTL:     cf.F(dns.TTL(c.ttl)),
			Proxied: cf.F(c.proxied),
		}
	default:
		// For other record types, use A record as fallback
		body = dns.ARecordParam{
			Type:    cf.F(dns.ARecordTypeA),
			Name:    cf.F(name),
			Content: cf.F(content),
			TTL:     cf.F(dns.TTL(c.ttl)),
			Proxied: cf.F(c.proxied),
		}
	}

	params := dns.RecordNewParams{
		ZoneID: cf.F(c.zoneID),
		Body:   body,
	}

	record, err := c.api.New(ctx, params)
	if err != nil {
		return dns.RecordResponse{}, errors.WithStack(err)
	}

	return *record, nil
}

func (c *DNSClient) UpdateDNSRecord(ctx context.Context, recordID, name, recordType, content string) (dns.RecordResponse, error) {
	// Build the record data based on type
	var body dns.RecordUpdateParamsBodyUnion
	switch recordType {
	case "A":
		body = dns.ARecordParam{
			Type:    cf.F(dns.ARecordTypeA),
			Name:    cf.F(name),
			Content: cf.F(content),
			TTL:     cf.F(dns.TTL(c.ttl)),
			Proxied: cf.F(c.proxied),
		}
	case "AAAA":
		body = dns.AAAARecordParam{
			Type:    cf.F(dns.AAAARecordTypeAAAA),
			Name:    cf.F(name),
			Content: cf.F(content),
			TTL:     cf.F(dns.TTL(c.ttl)),
			Proxied: cf.F(c.proxied),
		}
	default:
		// For other record types, use A record as fallback
		body = dns.ARecordParam{
			Type:    cf.F(dns.ARecordTypeA),
			Name:    cf.F(name),
			Content: cf.F(content),
			TTL:     cf.F(dns.TTL(c.ttl)),
			Proxied: cf.F(c.proxied),
		}
	}

	params := dns.RecordUpdateParams{
		ZoneID: cf.F(c.zoneID),
		Body:   body,
	}

	record, err := c.api.Update(ctx, recordID, params)
	if err != nil {
		return dns.RecordResponse{}, errors.WithStack(err)
	}

	return *record, nil
}

func (c *DNSClient) ReplaceRecords(ctx context.Context, name, recordType, newContent string) error {
	records, err := c.GetDNSRecords(ctx, name, recordType)
	if err != nil {
		return err
	}

	if len(records) == 0 {
		_, err = c.CreateDNSRecord(ctx, name, recordType, newContent)
		if err != nil {
			return err
		}
		return nil
	}

	if _, err = c.UpdateDNSRecord(ctx, records[0].ID, name, recordType, newContent); err != nil {
		return err
	}

	for _, record := range records[1:] {
		if err := c.DeleteDNSRecord(ctx, record.ID); err != nil {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}

	return nil
}
