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

## 2025-02-18 - Optimize commit statuses chunked queries with concurrency
**Learning:** `DecorateCommitsWithStatuses` fetches Firestore chunked queries sequentially. For PRs or branches with hundreds of commits, making ceiling(N/30) sequential HTTP queries to Firestore caused significant network I/O latency.
**Action:** When working around the Firestore 30-element `IN` query limit, parallelize the chunked queries using `golang.org/x/sync/errgroup` with a concurrency limit (`g.SetLimit(10)`). This drastically reduces total network latency for fetching commit statuses without causing socket exhaustion. Protect the aggregating maps using `sync.Mutex`.
