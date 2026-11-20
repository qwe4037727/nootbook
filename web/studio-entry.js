let studioProjectID = new URLSearchParams(location.search).get("projectId");

function studioProjectHeaders() {
  return studioProjectID ? { "X-Project-ID": studioProjectID } : {};
}

function studioProjectURL(path) {
  const url = new URL(path, location.origin);
  if (studioProjectID) url.searchParams.set("projectId", studioProjectID);
  return url.href;
}

if (studioProjectID) {
  document.cookie = `project_id=${encodeURIComponent(studioProjectID)}; path=/; max-age=31536000; SameSite=Lax`;
}
