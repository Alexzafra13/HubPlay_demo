import { describe, it, expect } from "vitest";
import { extractCode } from "./extractCode";

// extractCode es la parte pura del modal — vale la pena pinearla
// porque define qué QRs son "válidos" desde la perspectiva de
// LinkDevice. El modal en sí necesitaría jsdom con MediaStream
// faking, que se sale del scope para una mejora de UX.
describe("extractCode", () => {
  it("extrae el code de una verification_uri_complete con dash", () => {
    const url = "https://hubplay.example.com/link?code=ABCD-EFGH";
    expect(extractCode(url)).toBe("ABCD-EFGH");
  });

  it("extrae el code sin dash si el QR lo lleva canónico", () => {
    const url = "https://hubplay.example.com/link?code=ABCDEFGH";
    expect(extractCode(url)).toBe("ABCDEFGH");
  });

  it("acepta un código pelado sin URL alrededor", () => {
    expect(extractCode("ABCD-EFGH")).toBe("ABCD-EFGH");
    expect(extractCode("ABCDEFGH")).toBe("ABCDEFGH");
  });

  it("rechaza URLs sin code (otro QR de cualquier sitio)", () => {
    expect(extractCode("https://example.com")).toBeNull();
    expect(extractCode("https://hubplay.example.com/link")).toBeNull();
  });

  it("rechaza valores claramente no-code (vCard, texto suelto)", () => {
    expect(extractCode("hello world")).toBeNull();
    expect(extractCode("BEGIN:VCARD\nN:Doe\nEND:VCARD")).toBeNull();
    expect(extractCode("")).toBeNull();
  });

  it("rechaza un code de longitud inesperada en query string", () => {
    expect(
      extractCode("https://hubplay.example.com/link?code=ABCDEFG"),
    ).toBeNull(); // 7 chars
    expect(
      extractCode("https://hubplay.example.com/link?code=ABCDEFGHI"),
    ).toBeNull(); // 9 chars
  });

  it("trim espacios al borde y respeta el formato interno", () => {
    expect(extractCode("  ABCDEFGH  ")).toBe("ABCDEFGH");
  });
});
