// The client a chat turn comes from, sent as `channel` with every send and
// steer: "web" for frontend-next, "word_addin" for the Word add-in (set once
// at start-up). The server shows web-only cards, such as the agent hand-off,
// only to "web".
let channel = "web";

export function setClientChannel(value: string) {
  channel = value;
}

export function clientChannel() {
  return channel;
}
