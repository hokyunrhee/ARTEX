import { activeMention, mentionSearch, mentionToken, selectedMentions } from "./chat-mentions.ts";
import assert from "node:assert/strict";
import test from "node:test";

test("mention trigger supports cursor placement without hijacking email", () => {
  assert.equal(activeMention("user@example.com", 16), null);
  assert.equal(activeMention("selected @[finding#1 X]", 18), null);
  assert.deepEqual(activeMention("see @finding then text", 12), { start: 4, end: 12, query: "finding" });
  assert.equal(activeMention("@finding\nnext line", 12), null);
});

test("categories, aliases, IP and keyword search", () => {
  assert.equal(mentionSearch("").categories.length, 9);
  assert.equal(mentionSearch("fin").categories[0].kind, "finding");
  assert.equal(mentionSearch("finding").kind, "finding");
  assert.equal(mentionSearch("finding SQL injection").query, "SQL injection");
  assert.equal(mentionSearch("ip 192.0.2.1").kind, "ip");
  assert.equal(mentionSearch("api GET /api").query, "GET /api");
  assert.equal(mentionSearch("acme.com").kind, "");
});

test("legacy Chinese mention tokens still resolve on read", () => {
  // Conversations stored before the English conversion carry Chinese wire words.
  const legacy = selectedMentions("分析 @[漏洞#12 标题] 和 @[接口#34 GET /api]");
  assert.equal(legacy.length, 2);
  assert.equal(legacy[0].token, "@[漏洞#12 标题]");
  assert.equal(legacy[1].token, "@[接口#34 GET /api]");
});

test("tokens use the English alias wire word and removing one reference preserves its neighbors", () => {
  const first = mentionToken({ kind: "finding", id: 12, label: "title[1]\ndescription" });
  const second = mentionToken({ kind: "ip", id: 13, label: "192.0.2.1" });
  assert.equal(first, "@[finding#12 title（1） description]");
  const value = `analyze ${first} and ${second}`;
  const selected = selectedMentions(value);
  assert.equal(selected.length, 2);
  assert.equal(selected[0].label, "finding #12 · title（1） description");
  const next = value.slice(0, selected[0].start) + value.slice(selected[0].start + selected[0].token.length);
  assert.equal(selectedMentions(next)[0].token, second);
});
