import http from 'node:http';
import { readFile } from 'node:fs/promises';

const files = new Map([
  ['/', ['../site/index.html', 'text/html; charset=utf-8']],
  ['/index.html', ['../site/index.html', 'text/html; charset=utf-8']],
  ['/styles.css', ['../site/styles.css', 'text/css; charset=utf-8']],
  ['/assets/icon.svg', ['../assets/icon.svg', 'image/svg+xml']],
]);
const port = Number(process.argv[2] ?? 8080);
if (!Number.isInteger(port) || port < 0 || port > 65535) {
  throw new Error('Port must be an integer from 0 to 65535');
}

const server = http.createServer(async (request, response) => {
  if (!['GET', 'HEAD'].includes(request.method)) {
    response.writeHead(405, { Allow: 'GET, HEAD' });
    return response.end();
  }
  const file = files.get(new URL(request.url, 'http://localhost').pathname);
  if (!file) {
    response.writeHead(404);
    return response.end('Not found');
  }
  try {
    const content = await readFile(new URL(file[0], import.meta.url));
    response.writeHead(200, {
      'Content-Type': file[1], 'Content-Length': content.length,
      'Cache-Control': 'no-store', 'X-Content-Type-Options': 'nosniff',
    });
    response.end(request.method === 'HEAD' ? undefined : content);
  } catch {
    response.writeHead(500);
    response.end('Could not read site file');
  }
});
server.on('error', error => {
  console.error(error.message);
  process.exitCode = 1;
});
server.listen(port, '127.0.0.1', () => {
  console.log(`Preview: http://127.0.0.1:${server.address().port}/ (Ctrl+C to stop)`);
});
