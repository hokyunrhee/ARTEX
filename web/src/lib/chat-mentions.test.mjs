import { activeMention, mentionKinds, mentionSearch, mentionToken, selectedMentions } from "./chat-mentions.ts";
import assert from "node:assert/strict";
import test from "node:test";

test("mention trigger preserves legacy tokens and cursor placement without hijacking email", () => {
  assert.equal(activeMention("user@example.com", 16), null);
  assert.equal(activeMention("Selected @[漏洞#1 X]", 18), null);
  assert.deepEqual(activeMention("See @finding later", 12), { start: 4, end: 12, query: "finding" });
  assert.equal(activeMention("@finding\nNext line", 18), null);
});

test("categories, English labels, aliases, and keyword search", () => {
  assert.equal(mentionSearch("").categories.length, 9);
  assert.equal(mentionSearch("fi").categories[0].kind, "finding");
  assert.equal(mentionSearch("Finding").kind, "finding");
  assert.equal(mentionSearch("finding SQL injection").query, "SQL injection");
  assert.equal(mentionSearch("ip 192.0.2.1").kind, "ip");
  assert.equal(mentionSearch("API GET /api").query, "GET /api");
  assert.equal(mentionSearch("acme.com").kind, "");
});

test("all current and legacy wire words remain selectable", () => {
  const legacy = ["漏洞", "资产", "企业", "接口", "IP", "应用", "域名", "子域名", "服务"];
  for (const [index, kind] of mentionKinds.entries()) {
    const token = mentionToken({ kind: kind.kind, id: index + 1, label: "Record", description: "" });
    assert.equal(token, `@[${kind.alias}#${index + 1} Record]`);
    const oldToken = `@[${legacy[index]}#${index + 1} Record]`;
    assert.equal(selectedMentions(`${token} ${oldToken}`).length, 2);
  }
});

test("tokens roundtrip escaped labels and removing one reference preserves its neighbors", () => {
  const first = mentionToken({ kind: "finding", id: 12, label: "Title[1]\nDescription", description: "" });
  const second = mentionToken({ kind: "ip", id: 13, label: "192.0.2.1", description: "" });
  const value = `Analyze ${first} and ${second}`;
  const selected = selectedMentions(value);
  assert.equal(selected.length, 2);
  assert.equal(selected[0].label, "finding #12 · Title（1） Description");
  const next = value.slice(0, selected[0].start) + value.slice(selected[0].start + selected[0].token.length);
  assert.equal(selectedMentions(next)[0].token, second);
});
