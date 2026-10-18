package storefront

import (
	"context"
	"encoding/json"
	"testing"
)

type fakeScreening struct {
	flagged     bool
	moderateErr error
	resizeErr   error
	resizeCalls int
}

func (f *fakeScreening) Moderate(context.Context, []byte, string, string) (bool, error) {
	return f.flagged, f.moderateErr
}

func (f *fakeScreening) Resize(context.Context, []byte, string, string, string) (json.RawMessage, error) {
	f.resizeCalls++
	return json.RawMessage(`{"id":"img_example"}`), f.resizeErr
}

func TestListingDecision(t *testing.T) {
	tests := []struct {
		name         string
		flagged      bool
		wantListing  string
		wantCheckout string
		wantResize   int
	}{
		{name: "approved content is transformed and published", wantListing: "published", wantCheckout: "captured", wantResize: 1},
		{name: "flagged content is quarantined before transform", flagged: true, wantListing: "quarantined", wantCheckout: "authorized", wantResize: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeScreening{flagged: tt.flagged}
			workflow := NewListingWorkflow(client)
			got, err := workflow.Submit(context.Background(), ListingSubmission{
				OrderID: "ord_42", Caption: "Blue ceramic mug", Filename: "mug.jpg", MediaType: "image/jpeg", Image: []byte("image"),
			})
			if err != nil {
				t.Fatalf("Submit() error = %v", err)
			}
			if got.Listing != tt.wantListing || got.Checkout != tt.wantCheckout {
				t.Fatalf("state = listing %q checkout %q", got.Listing, got.Checkout)
			}
			if client.resizeCalls != tt.wantResize {
				t.Fatalf("Resize() calls = %d, want %d", client.resizeCalls, tt.wantResize)
			}
		})
	}
}
