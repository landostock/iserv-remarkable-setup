const HEADERS = {
  "Cache-Control": "no-store",
  "Content-Type": "text/plain; charset=utf-8",
  "X-Content-Type-Options": "nosniff",
};

export default {
  fetch() {
    return new Response("Setup is not complete.", { status: 503, headers: HEADERS });
  },
};
