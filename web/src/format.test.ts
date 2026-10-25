import { expect, test } from "vitest";
import { ms, percent, deltaMS, deltaPP, numeric } from "./format";
test("units preserve missing values, real zero, signed differences and percentage points", () => {
  expect(deltaMS(-0.004)).toBe("-<0.01 ms");
  expect(deltaPP(0.00001)).toBe("+<0.01 pp");
  expect(ms(-0)).toBe("0 ms");
  expect(ms()).toBe("—");
  expect(ms(0)).toBe("0 ms");
  expect(ms(0.004)).toBe("<0.01 ms");
  expect(ms(81.234)).toBe("81.23 ms");
  expect(percent(1)).toBe("100%");
  expect(percent(0.12)).toBe("12%");
  expect(deltaMS(80)).toBe("+80 ms");
  expect(deltaMS(-80)).toBe("-80 ms");
  expect(deltaMS(0)).toBe("0 ms");
  expect(deltaPP(0.25)).toBe("+25 pp");
  expect(deltaPP(-0.05)).toBe("-5 pp");
  expect(numeric(Infinity)).toBe("—");
});
