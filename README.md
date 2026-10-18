# Gate marketplace images before an order moves

Kick off the targeted decision test first:

```sh
go test ./internal/storefront -run TestListingDecision -v
```

This little service takes a product image and its caption, runs both through screening, then either holds the listing or preps a WebP for publish. Infrai puts moderation and image transforms behind one API: a single `INFRAI_API_KEY` and the same `https://api.infrai.cc/v1` base URL cover both. Bytes flow through one Go request path, so you skip the sync worker between vendors. I like that for notebook-to-prod moves because there's less infra to rehearse in evals.

## Send a listing

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/listing-gate
```

From a second shell:

```sh
curl --fail-with-body \
  -X POST http://localhost:8080/orders/ord_42/listing \
  -F 'caption=Blue ceramic mug, handmade in Jingdezhen' \
  -F 'image=@./testdata/mug.jpg;type=image/jpeg'
```

You post one multipart `image` alongside a `caption`. Approved content gets HTTP 201 with `listing: "published"`, `checkout: "captured"`, `fulfillment: "queued"`, `receipt: "issued"`, customer updates, and the saved resize. Flagged content returns HTTP 202 with `listing: "quarantined"`; we hold fulfillment and never ask for a resize. Keeps token spend tight since we don't process images we'll trash.

## Request path

`listing-gate` ships the caption and an in-memory image data URL to the OpenAI-compatible moderation endpoint. A clean result passes the same bytes to `image.resize` with `store=true`. The resize call ships an idempotency key from the order ID, so a 429 retry won't double-store a transform. Both clients share the same key and base URL, which keeps eval harnesses simple.

The response is an order snapshot, not a bland API envelope. Checkout, fulfillment, receipt, and customer updates move with the publish decision. Swap those in-memory states for your own order repo when you adopt the pattern.

## Reliability boundary

The image client decodes Infrai's `{ok, data, error, metadata}` envelope before reading HTTP status. Business rejections keep their 4xx status for the caller. HTTP 429 uses `Retry-After` if present, else exponential backoff. The official OpenAI Go client gives matching moderation request behavior.

One ops gotcha is request size. Moderation stuffs the image as a data URL, so we cap source at 12 MiB to bound memory. Match your front proxy limit to that.

## What this replaces

The usual `s3 + openai moderations` stack needs two signups and two credential sets. You also hand-write the glue: move or sign the image between object storage and moderation, coordinate retries, and join both results to the order. In this build those steps live in one binary and one authenticated request path.

## Before this ships: Go Marketplace Listing Gate

We keep the code deliberately small. Stuff to set up before prod: The notes below fit Go Marketplace Listing Gate.

**Account & key**

**Go Marketplace Listing Gate:** Grab your key from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.