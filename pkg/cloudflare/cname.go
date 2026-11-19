package cloudflare

import (
	"context"
	"fmt"
	"strings"

	cf "github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/dns"
)

const recordCNAME = "CNAME"

func (c *DNSClient) buildCNAMERecord(name, content string) dns.CNAMERecordParam {
	return dns.CNAMERecordParam{Type: cf.F(dns.CNAMERecordTypeCNAME), Name: cf.F(name), Content: cf.F(content), TTL: cf.F(dns.TTL(c.ttl)), Proxied: cf.F(c.proxied)}
}

// A CNAME is a single value: update its existing ID instead of creating a conflicting record first.
func (c *DNSClient) replaceCNAME(ctx context.Context, name string, contents []string) error {
	if len(contents) != 1 || contents[0] == "" {
		return fmt.Errorf("CNAME requires exactly one target")
	}
	records, err := c.GetDNSRecords(ctx, name, recordCNAME)
	if err != nil {
		return err
	}
	if len(records) > 1 {
		return fmt.Errorf("multiple CNAME records for %s", name)
	}
	if len(records) == 0 {
		_, err = c.CreateDNSRecord(ctx, name, recordCNAME, contents[0])
		return err
	}
	current := records[0]
	if strings.EqualFold(strings.TrimSuffix(current.Content, "."), strings.TrimSuffix(contents[0], ".")) && current.Proxied == c.proxied {
		return nil
	}
	_, err = c.UpdateDNSRecord(ctx, current.ID, name, recordCNAME, contents[0])
	return err
}
