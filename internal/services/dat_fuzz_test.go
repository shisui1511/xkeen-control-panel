package services

import (
	"testing"
)

func FuzzParseDATEntryDomains(f *testing.F) {
	dom1 := makeLD(2, []byte("google.com"))
	dom2 := makeLD(2, []byte("youtube.com"))
	entry1 := append(makeLD(1, []byte("google")), makeLD(2, dom1)...)
	entry1 = append(entry1, makeLD(2, dom2)...)
	outer := makeLD(1, entry1)

	f.Add(outer)
	f.Add([]byte("random data"))
	f.Add([]byte{0x0A, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x01})

	f.Fuzz(func(t *testing.T, data []byte) {
		parseDATEntryDomains(data)
	})
}

func FuzzParseDATEntryCIDRs(f *testing.F) {
	cidr1 := append(makeLD(1, []byte{8, 8, 8, 8}), makeVarintField(2, 32)...)
	cidr2 := append(makeLD(1, []byte{1, 1, 1, 1}), makeVarintField(2, 24)...)
	entry1 := append(makeLD(1, []byte("google")), makeLD(2, cidr1)...)
	entry1 = append(entry1, makeLD(2, cidr2)...)
	outer := makeLD(1, entry1)

	f.Add(outer)
	f.Add([]byte("random data"))
	f.Add([]byte{0x0A, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x01})

	f.Fuzz(func(t *testing.T, data []byte) {
		parseDATEntryCIDRs(data)
	})
}
