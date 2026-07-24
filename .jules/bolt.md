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

## 2025-02-18 - Extract complex list items into memoized components to avoid cascading O(N) re-renders
**Learning:** Found massive UI lag on Pull Request Detail pages when viewing large diffs. Interacting with a single file (like toggling collapse or viewed status) would update a state variable in the main \`PullRequestDetail\` component, triggering a cascading O(N) re-render of thousands of diff lines for *all* files. Inline \`.map()\` rendering for complex DOM trees kills React performance.
**Action:** Extract expensive list items (like \`DiffFileCard\`) into separate components, wrap them in \`React.memo\`, and pass stable \`React.useCallback\` functions for state updates to prevent unaffected elements from re-rendering.

## 2025-02-18 - Pre-aggregate tree stats incrementally to avoid recursive evaluation bottlenecks
**Learning:** Found an `O(N * D)` algorithmic bottleneck in `frontend/src/pages/Repository.jsx` `calculateTreeStats` where tree nodes recursively aggregated child stats after the tree was built. For PRs with deep directory structures and many files, this caused significant redundant calculations.
**Action:** When constructing deep hierarchies (e.g., file trees) where parent nodes depend on child node metrics, pre-aggregate these sums incrementally during the initial creation phase to avoid recursive evaluation bottlenecks.
