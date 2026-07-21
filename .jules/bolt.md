## 2025-02-18 - Optimize file tree filtering in PR detailed view
**Learning:** Found an O(N * D) algorithmic bottleneck in `frontend/src/pages/Repository.jsx` where the `filteredTree` variable used nested loops across a recursive function inside `copyAndFilter` when searching the diff tree. It redundantly evaluated the same nodes.
**Action:** Replace nested `checkMatch` and `copyAndFilter` with a single recursive single-pass traversal `filterNode(nodeList)`. It filters children and evaluates whether to retain a directory based on child matches or folder paths, resulting in a clean O(N) filtering strategy.
- When optimizing I/O intensive loops, carefully consider using golang.org/x/sync/errgroup to bounded parallelization with early cancellation and context propagation.

## 2025-02-18 - Optimize diff file tree parsing in PR detail view
**Learning:** Found an `O(N * D)` algorithmic bottleneck in `frontend/src/pages/Repository.jsx` `buildFileTree` where tree nodes were being constructed by recursively doing a linear array search `current.children.find(...)` at each level of depth. For PRs with 1000s of files in nested directories, this became extremely slow.
**Action:** Replaced the array search with a flat `Map` to lookup `path -> node` references in `O(1)` time. Nested array searches inside tree construction are silent bottlenecks; always map paths to nodes ahead of time or concurrently.

## 2025-02-18 - Optimize commit statuses query using chunked IN queries
**Learning:** `DecorateCommitsWithStatuses` previously fetched all commit statuses for an entire repository unbounded. For large repositories, pulling every single status and evaluating it in memory causes massive memory usage, high latency, and excessive Firestore egress costs.
**Action:** Always filter Firestore queries explicitly by the entities requested. Because Firestore `in` queries have a hard limit of 30 items, when querying for an array of items (like a batch of SHAs), iterate over the input array and chunk it into batches of `min(len, 30)`, merging the results locally.
## 2024-03-08 - Concurrent Firestore Chunk Queries
**Learning:** Firestore `in` queries are limited to 30 elements, forcing sequential queries for large datasets which causes significant network latency. Go 1.25 handles loop variable captures cleanly, making closure usage safer than previous versions.
**Action:** When querying large batches in Firestore, use `golang.org/x/sync/errgroup` with a concurrency limit (e.g., `g.SetLimit(10)`) to concurrently execute chunked queries. Move document parsing outside of mutex locks to reduce lock contention and maximize parallel throughput.
## 2025-02-18 - Optimize React sorting with Schwartzian transforms
**Learning:** Found O(N log N) bottlenecks when rendering sorted lists (like the repository list in Dashboard or branch protection rules in Repository) because expensive sorting keys were being re-calculated on every comparison. The sorting callbacks did string manipulations, looping, or `Date.parse()` evaluations every time they compared elements.
**Action:** Use the Schwartzian transform (decorate-sort-undecorate) pattern to optimize sorting in Javascript whenever the sort key computation is expensive. Map the array to objects that hold the original element and the pre-computed sort keys, sort the mapped array, and map back to the original element list.
## 2025-02-18 - Memoize list items to prevent cascading re-renders
**Learning:** Found a severe performance bottleneck when interacting with large PR diff views. The inline `.map` for `filteredFiles` in `Repository.jsx` meant that toggling the "Viewed" or "Collapse" state for a *single* file caused the *entire list* of O(N) file diffs to re-render from scratch, leading to multi-second UI hangs on PRs with hundreds of files.
**Action:** Extract list item rendering into separate components and wrap them in `React.memo` with a custom equality function matching scalar/stable props. Additionally, always wrap callback functions (like toggle handlers) passed to these items in `React.useCallback` to ensure stable references and avoid breaking memoization.
