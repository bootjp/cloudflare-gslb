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
	ReplaceRecords(ctx context.Context, name, recordType string, newContents []string) error
	GetZoneID() string
}

type DNSClient struct {
	api     cloudflareAPI
	zoneID  string
	proxied bool
	ttl     int
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

func (c *DNSClient) buildARecord(name, content string) dns.ARecordParam {
	return dns.ARecordParam{
		Type:    cf.F(dns.ARecordTypeA),
		Name:    cf.F(name),
		Content: cf.F(content),
		TTL:     cf.F(dns.TTL(c.ttl)),
		Proxied: cf.F(c.proxied),
	}
}

func (c *DNSClient) buildAAAARecord(name, content string) dns.AAAARecordParam {
	return dns.AAAARecordParam{
		Type:    cf.F(dns.AAAARecordTypeAAAA),
		Name:    cf.F(name),
		Content: cf.F(content),
		TTL:     cf.F(dns.TTL(c.ttl)),
		Proxied: cf.F(c.proxied),
	}
}

func (c *DNSClient) CreateDNSRecord(ctx context.Context, name, recordType, content string) (dns.RecordResponse, error) {
	var body dns.RecordNewParamsBodyUnion
	switch recordType {
	case "A":
		body = c.buildARecord(name, content)
	case "AAAA":
		body = c.buildAAAARecord(name, content)
	default:
		body = c.buildARecord(name, content)
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
	var body dns.RecordUpdateParamsBodyUnion
	switch recordType {
	case "A":
		body = c.buildARecord(name, content)
	case "AAAA":
		body = c.buildAAAARecord(name, content)
	default:
		body = c.buildARecord(name, content)
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

func (c *DNSClient) deleteRecords(ctx context.Context, recordsToDelete []dns.RecordResponse) error {
	for _, record := range recordsToDelete {
		if err := c.DeleteDNSRecord(ctx, record.ID); err != nil {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

func (c *DNSClient) ReplaceRecords(ctx context.Context, name, recordType string, newContents []string) error {
	if len(newContents) == 0 {
		return errors.New("no record contents provided")
	}

	desired := dedupeContents(newContents)

	records, err := c.GetDNSRecords(ctx, name, recordType)
	if err != nil {
		return err
	}

	if len(records) == 0 {
		return c.createRecords(ctx, name, recordType, desired)
	}

	desiredSet := buildContentSet(desired)
	recordsByContent := groupRecordsByContent(records)
	missing, recordsToDelete := diffRecords(desired, desiredSet, recordsByContent)

	if err := c.createRecords(ctx, name, recordType, missing); err != nil {
		return err
	}

	if len(recordsToDelete) == 0 {
		return nil
	}

	return c.deleteRecords(ctx, recordsToDelete)
}

func (c *DNSClient) createRecords(ctx context.Context, name, recordType string, contents []string) error {
	for _, content := range contents {
		if _, err := c.CreateDNSRecord(ctx, name, recordType, content); err != nil {
			return err
		}
	}
	return nil
}

func buildContentSet(contents []string) map[string]struct{} {
	desiredSet := make(map[string]struct{}, len(contents))
	for _, content := range contents {
		desiredSet[content] = struct{}{}
	}
	return desiredSet
}

func groupRecordsByContent(records []dns.RecordResponse) map[string][]dns.RecordResponse {
	recordsByContent := make(map[string][]dns.RecordResponse, len(records))
	for _, record := range records {
		recordsByContent[record.Content] = append(recordsByContent[record.Content], record)
	}
	return recordsByContent
}

func diffRecords(desired []string, desiredSet map[string]struct{}, recordsByContent map[string][]dns.RecordResponse) ([]string, []dns.RecordResponse) {
	var recordsToDelete []dns.RecordResponse
	var missing []string

	for _, content := range desired {
		existing := recordsByContent[content]
		if len(existing) == 0 {
			missing = append(missing, content)
			continue
		}
		if len(existing) > 1 {
			recordsToDelete = append(recordsToDelete, existing[1:]...)
		}
	}

	for content, existing := range recordsByContent {
		if _, ok := desiredSet[content]; !ok {
			recordsToDelete = append(recordsToDelete, existing...)
		}
	}

	return missing, recordsToDelete
}

func dedupeContents(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
