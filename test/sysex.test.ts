import { describe, expect, test } from "bun:test";
import * as sx from "../src/sysex";

describe("sysex", () => {
  test("チートシートのバイト列と一致する", () => {
    expect(sx.toHex(sx.getCurrentProgram(0x6e))).toBe("f0 52 00 6e 33 f7");
    expect(sx.toHex(sx.requestCurrentPatch(0x6e))).toBe("f0 52 00 6e 64 13 f7");
    expect(sx.toHex(sx.requestPatch(0x6e, 1, 130))).toBe("f0 52 00 6e 46 00 00 01 00 02 01 f7");
  });

  test("Identity Reply を解釈する", () => {
    const ident = sx.parseIdentityReply(sx.fromHex("f0 7e 00 06 02 52 6e 00 00 00 31 2e 31 30 f7"));
    expect(ident.manufacturer).toBe(sx.ZOOM_MANUFACTURER);
    expect(ident.deviceId).toBe(0x6e);
    expect(ident.version).toBe("1.10");
  });

  test("Identity Reply 以外は弾く", () => {
    expect(() => sx.parseIdentityReply(sx.fromHex("f0 52 00 6e 33 f7"))).toThrow();
  });
});
