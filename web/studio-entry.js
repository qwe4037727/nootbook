(() => {
  const url = new URL(location.href);
  const id = url.searchParams.get('projectId');
  if (!id) return;
  document.cookie = `project_id=${encodeURIComponent(id)}; path=/; max-age=31536000; SameSite=Lax`;
  url.searchParams.delete('projectId');
  history.replaceState(history.state, '', url);
})();
