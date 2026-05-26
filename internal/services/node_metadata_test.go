package services

import (
	"testing"
)

func TestParseRemark(t *testing.T) {
	tests := []struct {
		remark   string
		expected SubscriptionNode
	}{
		{
			remark: "RU-Moscow | Youtube, Netflix",
			expected: SubscriptionNode{
				Name:    "Moscow",
				Country: "RU",
				Flag:    "🇷🇺",
				UseCase: "Youtube, Netflix",
			},
		},
		{
			remark: "DE-Frankfurt [NEW] (Youtube, Instagram) 10Gb/s",
			expected: SubscriptionNode{
				Name:    "Frankfurt",
				Country: "DE",
				Flag:    "🇩🇪",
				UseCase: "Youtube, Instagram",
				Speed:   "10Gb/s",
				IsNew:   true,
			},
		},
		{
			remark: "🇷🇺 Russia - St. Petersburg",
			expected: SubscriptionNode{
				Name:    "Russia - St. Petersburg", // страна убрана из базы, поэтому в имени останется Russia
				Country: "RU",
				Flag:    "🇷🇺",
			},
		},
		{
			remark: "NL - Amsterdam (Gaming) 1Gb/s",
			expected: SubscriptionNode{
				Name:    "Amsterdam",
				Country: "NL",
				Flag:    "🇳🇱",
				UseCase: "Gaming",
				Speed:   "1Gb/s",
			},
		},
		{
			remark: "🆕 US-New York | ChatGPT",
			expected: SubscriptionNode{
				Name:    "New York",
				Country: "US",
				Flag:    "🇺🇸",
				UseCase: "ChatGPT",
				IsNew:   true,
			},
		},
		{
			remark: "dubai (Socials)",
			expected: SubscriptionNode{
				Name:    "dubai",
				UseCase: "Socials",
			},
		},
		{
			remark: "custom-node-without-metadata",
			expected: SubscriptionNode{
				Name: "custom-node-without-metadata",
			},
		},
		{
			remark: "Нидерланды 2 [NEW]",
			expected: SubscriptionNode{
				Name:  "Нидерланды 2",
				IsNew: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.remark, func(t *testing.T) {
			actual := parseRemark(tt.remark)
			if actual.Name != tt.expected.Name {
				t.Errorf("expected Name %q, got %q", tt.expected.Name, actual.Name)
			}
			if actual.Country != tt.expected.Country {
				t.Errorf("expected Country %q, got %q", tt.expected.Country, actual.Country)
			}
			if actual.Flag != tt.expected.Flag {
				t.Errorf("expected Flag %q, got %q", tt.expected.Flag, actual.Flag)
			}
			if actual.UseCase != tt.expected.UseCase {
				t.Errorf("expected UseCase %q, got %q", tt.expected.UseCase, actual.UseCase)
			}
			if actual.Speed != tt.expected.Speed {
				t.Errorf("expected Speed %q, got %q", tt.expected.Speed, actual.Speed)
			}
			if actual.IsNew != tt.expected.IsNew {
				t.Errorf("expected IsNew %v, got %v", tt.expected.IsNew, actual.IsNew)
			}
		})
	}
}
