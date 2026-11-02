export type Wire = {
  message_id?: string;
  session_id?: string;
  seq?: number;
  from?: string;
  to?: string;
  to_type?: string;
  msg_type?: number;
  body?: string;
  sent_at?: number;
  ack_message_id?: string;
  client_msg_id?: string;
  trace_id?: string;
  last_seq?: number;
  status?: string;
};
const fields = [
  "",
  "message_id",
  "session_id",
  "seq",
  "from",
  "to",
  "to_type",
  "msg_type",
  "body",
  "sent_at",
  "ack_message_id",
  "client_msg_id",
  "trace_id",
  "last_seq",
];
const strings = new Set([1, 2, 4, 5, 6, 8, 10, 11, 12]);
function varint(v: number) {
  let n = BigInt(Math.max(0, v));
  const b: number[] = [];
  while (n > 127n) {
    b.push(Number(n & 127n) | 128);
    n >>= 7n;
  }
  b.push(Number(n));
  return b;
}
export function encode(m: Wire) {
  const out: number[] = [];
  for (let i = 1; i < fields.length; i++) {
    const value = m[fields[i]];
    if (value === undefined || value === "") continue;
    if (strings.has(i)) {
      const b = new TextEncoder().encode(value);
      out.push(...varint(i * 8 + 2), ...varint(b.length), ...b);
    } else out.push(...varint(i * 8), ...varint(Number(value)));
  }
  return new Uint8Array(out);
}
export function decode(data: ArrayBuffer): Wire {
  const b = new Uint8Array(data);
  let p = 0;
  const m: Wire = {};
  function read() {
    let v = 0n,
      shift = 0n;
    for (let i = 0; i < 10 && p < b.length; i++) {
      const x = b[p++];
      v |= BigInt(x & 127) << shift;
      if (!(x & 128)) return Number(v);
      shift += 7n;
    }
    throw Error("invalid protobuf");
  }
  while (p < b.length) {
    const tag = read(),
      i = tag >> 3,
      wt = tag & 7;
    let v: any;
    if (wt === 0) v = read();
    else if (wt === 2) {
      const n = read();
      if (p + n > b.length) throw Error("invalid protobuf length");
      v = new TextDecoder().decode(b.slice(p, p + n));
      p += n;
    } else throw Error("unsupported protobuf field");
    if (fields[i]) m[fields[i]] = v;
  }
  return m;
}
