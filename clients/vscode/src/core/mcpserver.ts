import * as http from 'http';
import * as crypto from 'crypto';
import { Ide, callHand, handTools } from './hand';

/**
 * The hand, served over HTTP, speaking MCP.
 *
 * ⚠ **Loopback only.** This exists for one daemon on this machine and has no reason to be reachable
 * by anyone else. But loopback is not as narrow as the unix socket the rest of the protocol uses —
 * every other process on this machine can reach a loopback port — so a token goes out with the
 * attach (`mcp-attach` carries headers) and every request is checked against it.
 *
 * **No SSE.** The core's HTTP transport accepts a single `application/json` reply as well as
 * `text/event-stream`, and there is nothing here to stream. Implementing the path we do not use
 * would be code that first breaks when somebody finally needs it.
 *
 * Four methods, because the core's client calls four: `initialize`, `notifications/initialized`,
 * `tools/list`, `tools/call`.
 */
export class Hand {
  private constructor(private readonly server: http.Server, readonly token: string, readonly port: number) {}

  /** The address to hand the daemon. Port 0, then read back what was actually granted. */
  get url(): string { return `http://127.0.0.1:${this.port}/mcp`; }

  get headers(): Record<string, string> { return { 'X-Magi-Hand': this.token }; }

  static start(ide: Ide): Promise<Hand> {
    const token = crypto.randomUUID();
    return new Promise((resolve, reject) => {
      const server = http.createServer((req, res) => void serve(ide, token, req, res));
      server.once('error', reject);
      // Loopback by name, not 0.0.0.0: binding every interface would put this on the network.
      server.listen(0, '127.0.0.1', () => {
        const addr = server.address();
        if (!addr || typeof addr === 'string') { reject(new Error('the hand did not get a port')); return; }
        resolve(new Hand(server, token, addr.port));
      });
    });
  }

  close(): void { this.server.close(); }
}

/** The protocol revision the core's client speaks. Matched exactly — a mismatch can be refused. */
const PROTOCOL = '2025-06-18';

async function serve(ide: Ide, token: string, req: http.IncomingMessage, res: http.ServerResponse): Promise<void> {
  if (req.headers['x-magi-hand'] !== token) {
    // 403 and nothing else. A loopback port is reachable by every process on this machine, and a
    // caller without the token is not the daemon this hand was attached to.
    res.writeHead(403).end();
    return;
  }
  let body = '';
  for await (const chunk of req) body += chunk;
  let id: unknown = null;
  try {
    const msg = JSON.parse(body || '{}') as { id?: unknown; method?: string; params?: unknown };
    id = msg.id ?? null;
    const method = msg.method ?? '';
    if (method === 'notifications/initialized') {
      // A notification has no reply. 204 rather than a body: the core accepts 202/200/204 here.
      res.writeHead(204).end();
      return;
    }
    const result = await dispatch(ide, method, msg.params);
    send(res, { jsonrpc: '2.0', id, result });
  } catch (e) {
    // A protocol error is not an HTTP error. A 500 reads to the client as the transport being
    // broken, which sends somebody looking at the wrong layer.
    send(res, {
      jsonrpc: '2.0', id,
      error: { code: -32603, message: e instanceof Error ? e.message : String(e) },
    });
  }
}

async function dispatch(ide: Ide, method: string, params: unknown): Promise<unknown> {
  switch (method) {
    case 'initialize':
      return {
        protocolVersion: PROTOCOL,
        capabilities: { tools: {} },
        serverInfo: { name: 'magi-vscode', version: '0.1.0' },
      };
    case 'tools/list':
      return {
        tools: handTools().map((t) => ({
          name: t.name,
          description: t.description,
          inputSchema: t.schema,
          // The declaration the core reads. Without it the protocol's default applies and `show`
          // lands in the record as "this turn changed that file".
          annotations: { readOnlyHint: t.readOnly },
        })),
      };
    case 'tools/call': {
      const p = (params ?? {}) as { name?: string; arguments?: Record<string, unknown> };
      const answer = await callHand(ide, p.name ?? '', p.arguments ?? {});
      // A failure is `isError` on the RESULT, not a JSON-RPC error: the call reached us and was
      // understood. The model reads isError and stops; a transport error would make it retry.
      return { content: [{ type: 'text', text: answer.text }], isError: answer.error === true };
    }
    default:
      throw new Error(`no such method "${method}"`);
  }
}

function send(res: http.ServerResponse, body: unknown): void {
  const text = JSON.stringify(body);
  res.writeHead(200, { 'content-type': 'application/json', 'content-length': Buffer.byteLength(text) }).end(text);
}
