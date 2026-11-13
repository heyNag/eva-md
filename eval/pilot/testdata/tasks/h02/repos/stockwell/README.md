# stockwell

A shelf of stock counts by SKU.

- `New()` returns an empty shelf.
- `Add(sku, n)` adds n units of sku.
- `Remove(sku, n)` takes n units away. It fails with ErrShort, and changes
  nothing, when the shelf holds fewer.
- `Total()` is the number of units on the shelf.
- `ReorderBelow(limit)` lists, sorted, the SKUs whose count is below limit.
  A SKU whose count fell to 0 is listed too.
- `MergeShelf(other)` adds every count of other to the shelf and leaves
  other as it was.
