---
type: system
title: Orders service
description: Takes, prices and tracks customer orders
status: stable
generated: { by: claude-code/claude-opus-5-5 }
sources:
  - resource: "github.com/acme/orders@4e2bf4c:internal/orders/"
  - resource: "https://docs.example.com/eng/orders/overview"
repos: [git@github.com:acme/orders.git]
aliases: [order service]
as_of: 2026-10-30
stale_after: "2027-01-31T00:00:00Z"
state: active
---

## Summary
Orders takes an order, prices it with Payments and tracks it to delivery.

## Interfaces
- `POST /v1/orders` creates an order
