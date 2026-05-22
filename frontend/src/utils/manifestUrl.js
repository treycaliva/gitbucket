// Parses the ?manifest= query parameter into a manifest object.
// The agent URL-encodes a JSON blob; we decode once.
export function parseManifestFromURL(searchString) {
  const params = new URLSearchParams(searchString);
  const raw = params.get('manifest');
  if (!raw) return null;
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}
