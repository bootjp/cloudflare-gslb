package cloudflare

import (
	"context"
	"fmt"
	"log"
	"time"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/dns"
	"github.com/cloudflare/cloudflare-go/v6/option"
	"github.com/cloudflare/cloudflare-go/v6/packages/pagination"
	"github.com/cockroachdb/errors"
)

// Sentinel errors for error comparison using errors.Is
var (
	ErrDeleteRecordsFailed = errors.New("failed to delete DNS records")
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
	ReplaceRecordsMultiple(ctx context.Context, name, recordType string, newContents []string) error
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

func (c *DNSClient) findRecordsToReplace(records []dns.RecordResponse, newContent string) (bool, []dns.RecordResponse) {
	var recordsToDelete []dns.RecordResponse
	foundMatch := false

	for i := range records {
		if records[i].Content == newContent {
			if !foundMatch {
				foundMatch = true
			} else {
				recordsToDelete = append(recordsToDelete, records[i])
			}
		} else {
			recordsToDelete = append(recordsToDelete, records[i])
		}
	}

	return foundMatch, recordsToDelete
}

// deleteRecords deletes all specified records. It continues deleting even if some deletions fail,
// collecting all errors and returning an aggregate error at the end. This ensures maximum cleanup
// even in the presence of partial failures.
// The returned error wraps ErrDeleteRecordsFailed for comparison using errors.Is.
func (c *DNSClient) deleteRecords(ctx context.Context, recordsToDelete []dns.RecordResponse) error {
	var deleteErrors []error
	for _, record := range recordsToDelete {
		if err := c.DeleteDNSRecord(ctx, record.ID); err != nil {
			deleteErrors = append(deleteErrors, fmt.Errorf("failed to delete record %s: %w", record.ID, err))
		}
		time.Sleep(500 * time.Millisecond)
	}
	if len(deleteErrors) > 0 {
		return errors.Wrapf(ErrDeleteRecordsFailed, "failed to delete %d record(s): %v", len(deleteErrors), deleteErrors)
	}
	return nil
}

func (c *DNSClient) ReplaceRecords(ctx context.Context, name, recordType, newContent string) error {
	records, err := c.GetDNSRecords(ctx, name, recordType)
	if err != nil {
		return err
	}

	// If no records exist, create one and return
	if len(records) == 0 {
		_, err = c.CreateDNSRecord(ctx, name, recordType, newContent)
		return err
	}

	foundMatch, recordsToDelete := c.findRecordsToReplace(records, newContent)

	// If no record has the desired content, create a new one first (atomic approach)
	if !foundMatch {
		_, err := c.CreateDNSRecord(ctx, name, recordType, newContent)
		if err != nil {
			return err
		}
		recordsToDelete = records
	}

	return c.deleteRecords(ctx, recordsToDelete)
}

// ReplaceRecordsMultiple replaces all records with the given contents (supports multiple IPs with the same priority).
//
// Error Handling:
// - If creating a new record fails, any successfully created records are rolled back (best-effort).
// - If deletion of old records fails after successfully creating new records, the function continues
//   deleting remaining records and returns an aggregate error. Both old and new records may exist
//   temporarily until all deletions succeed.
func (c *DNSClient) ReplaceRecordsMultiple(ctx context.Context, name, recordType string, newContents []string) error {
	if len(newContents) == 0 {
		log.Printf("Warning: ReplaceRecordsMultiple called with empty contents for %s (%s)", name, recordType)
		return nil
	}

	// If only one content, use the single record replace method
	if len(newContents) == 1 {
		return c.ReplaceRecords(ctx, name, recordType, newContents[0])
	}

	records, err := c.GetDNSRecords(ctx, name, recordType)
	if err != nil {
		return err
	}

	// Create a map of desired contents for quick lookup
	desiredContents := make(map[string]bool)
	for _, content := range newContents {
		desiredContents[content] = true
	}

	// Find records to keep, records to delete, and contents to create
	contentsToCreate := make(map[string]bool)
	for _, content := range newContents {
		contentsToCreate[content] = true
	}

	var recordsToDelete []dns.RecordResponse
	for _, record := range records {
		if desiredContents[record.Content] {
			// This record has desired content, keep it and remove from create list
			delete(contentsToCreate, record.Content)
		} else {
			// This record doesn't have desired content, delete it
			recordsToDelete = append(recordsToDelete, record)
		}
	}

	// Create new records for contents that don't exist
	var createdRecords []dns.RecordResponse
	for content := range contentsToCreate {
		record, err := c.CreateDNSRecord(ctx, name, recordType, content)
		if err != nil {
			// Best-effort rollback: delete any records that were successfully created
			if len(createdRecords) > 0 {
				if delErr := c.deleteRecords(ctx, createdRecords); delErr != nil {
					log.Printf("Warning: failed to rollback created DNS records for %s (%s): %v", name, recordType, delErr)
				}
			}
			return err
		}
		createdRecords = append(createdRecords, record)
	}

	// Delete records that are no longer needed
	return c.deleteRecords(ctx, recordsToDelete)
}
