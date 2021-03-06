// Generate zonefiles.
// This generates a zonefile that prioritizes beauty over efficiency.
package bind

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"

	"github.com/miekg/dns"
	"github.com/miekg/dns/dnsutil"
)

type zoneGenData struct {
	Origin     string
	DefaultTtl uint32
	Records    []dns.RR
}

func (z *zoneGenData) Len() int      { return len(z.Records) }
func (z *zoneGenData) Swap(i, j int) { z.Records[i], z.Records[j] = z.Records[j], z.Records[i] }
func (z *zoneGenData) Less(i, j int) bool {
	//fmt.Printf("DEBUG: i=%#v j=%#v\n", i, j)
	//fmt.Printf("DEBUG: z.Records=%#v\n", len(z.Records))
	a, b := z.Records[i], z.Records[j]
	//fmt.Printf("DEBUG: a=%#v b=%#v\n", a, b)
	compA, compB := dnsutil.AddOrigin(a.Header().Name, z.Origin+"."), dnsutil.AddOrigin(b.Header().Name, z.Origin+".")
	if compA != compB {
		if compA == z.Origin+"." {
			compA = "@"
		}
		if compB == z.Origin+"." {
			compB = "@"
		}
		return zoneLabelLess(compA, compB)
	}
	rrtypeA, rrtypeB := a.Header().Rrtype, b.Header().Rrtype
	if rrtypeA != rrtypeB {
		return zoneRrtypeLess(rrtypeA, rrtypeB)
	}
	if rrtypeA == dns.TypeA {
		ta2, tb2 := a.(*dns.A), b.(*dns.A)
		ipa, ipb := ta2.A.To4(), tb2.A.To4()
		if ipa == nil || ipb == nil {
			log.Fatalf("should not happen: IPs are not 4 bytes: %#v %#v", ta2, tb2)
		}
		return bytes.Compare(ipa, ipb) == -1
	}
	if rrtypeA == dns.TypeMX {
		ta2, tb2 := a.(*dns.MX), b.(*dns.MX)
		pa, pb := ta2.Preference, tb2.Preference
		return pa < pb
	}
	return a.String() < b.String()
}

// WriteZoneFile writes a beautifully formatted zone file.
func WriteZoneFile(w io.Writer, records []dns.RR, origin string, defaultTtl uint32) error {
	// This function prioritizes beauty over efficiency.
	// * The zone records are sorted by label, grouped by subzones to
	//   be easy to read and pleasant to the eye.
	// * Within a label, SOA and NS records are listed first.
	// * MX records are sorted numericly by preference value.
	// * A records are sorted by IP address, not lexicographically.
	// * Repeated labels are removed.
	// * $TTL is used to eliminate clutter.
	// * "@" is used instead of the apex domain name.

	z := &zoneGenData{
		Origin:     origin,
		DefaultTtl: defaultTtl,
	}
	z.Records = nil
	for _, r := range records {
		z.Records = append(z.Records, r)
	}
	return z.generateZoneFileHelper(w)
}

// generateZoneFileHelper creates a pretty zonefile.
func (z *zoneGenData) generateZoneFileHelper(w io.Writer) error {

	nameShortPrevious := ""

	sort.Sort(z)
	fmt.Fprintln(w, "$TTL", z.DefaultTtl)
	for i, rr := range z.Records {
		line := rr.String()
		if line[0] == ';' {
			continue
		}
		hdr := rr.Header()

		items := strings.SplitN(line, "\t", 5)
		if len(items) < 5 {
			log.Fatalf("Too few items in: %v", line)
		}

		// items[0]: name
		nameFqdn := hdr.Name
		nameShort := dnsutil.TrimDomainName(nameFqdn, z.Origin)
		name := nameShort
		if i > 0 && nameShort == nameShortPrevious {
			name = ""
		} else {
			name = nameShort
		}
		nameShortPrevious = nameShort

		// items[1]: ttl
		ttl := ""
		if hdr.Ttl != z.DefaultTtl && hdr.Ttl != 0 {
			ttl = items[1]
		}

		// items[2]: class
		if hdr.Class != dns.ClassINET {
			log.Fatalf("Unimplemented class=%v", items[2])
		}

		// items[3]: type
		typeStr := dns.TypeToString[hdr.Rrtype]

		// items[4]: the remaining line
		target := items[4]
		//if typeStr == "TXT" {
		//	fmt.Printf("generateZoneFileHelper.go: target=%#v\n", target)
		//}

		fmt.Fprintln(w, formatLine([]int{10, 5, 2, 5, 0}, []string{name, ttl, "IN", typeStr, target}))
	}
	return nil
}

func formatLine(lengths []int, fields []string) string {
	c := 0
	result := ""
	for i, length := range lengths {
		item := fields[i]
		for len(result) < c {
			result += " "
		}
		if item != "" {
			result += item + " "
		}
		c += length + 1
	}
	return strings.TrimRight(result, " ")
}

func zoneLabelLess(a, b string) bool {
	// Compare two zone labels for the purpose of sorting the RRs in a Zone.

	// If they are equal, we are done. All other code is simplified
	// because we can assume a!=b.
	if a == b {
		return false
	}

	// Sort @ at the top, then *, then everything else lexigraphically.
	// i.e. @ always is less. * is is less than everything but @.
	if a == "@" {
		return true
	}
	if b == "@" {
		return false
	}
	if a == "*" {
		return true
	}
	if b == "*" {
		return false
	}

	// Split into elements and match up last elements to first. Compare the
	// first non-equal elements.

	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	ia := len(as) - 1
	ib := len(bs) - 1

	var min int
	if ia < ib {
		min = len(as) - 1
	} else {
		min = len(bs) - 1
	}

	// Skip the matching highest elements, then compare the next item.
	for i, j := ia, ib; min >= 0; i, j, min = i-1, j-1, min-1 {
		if as[i] != bs[j] {
			return as[i] < bs[j]
		}
	}
	// The min top elements were equal, so the shorter name is less.
	return ia < ib
}

func zoneRrtypeLess(a, b uint16) bool {
	// Compare two RR types for the purpose of sorting the RRs in a Zone.

	// If they are equal, we are done. All other code is simplified
	// because we can assume a!=b.
	if a == b {
		return false
	}

	// List SOAs, then NSs, then all others.
	// i.e. SOA is always less. NS is less than everything but SOA.
	if a == dns.TypeSOA {
		return true
	}
	if b == dns.TypeSOA {
		return false
	}
	if a == dns.TypeNS {
		return true
	}
	if b == dns.TypeNS {
		return false
	}
	return a < b
}
