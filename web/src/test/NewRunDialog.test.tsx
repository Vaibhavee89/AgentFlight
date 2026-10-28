import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { NewRunDialog } from "../components/NewRunDialog";
import { bodyOf, json, mockFetch } from "./fetchMock";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function Probe() {
  return <p data-testid="location">{useLocation().pathname}</p>;
}

function renderDialog(onClose = vi.fn()) {
  render(
    <MemoryRouter initialEntries={["/runs"]}>
      <Routes>
        <Route path="/runs" element={<NewRunDialog onClose={onClose} />} />
        <Route path="/runs/:id" element={<p>detail page</p>} />
      </Routes>
      <Probe />
    </MemoryRouter>,
  );
  return onClose;
}

describe("NewRunDialog", () => {
  it("keeps Start disabled until the prompt has text", () => {
    renderDialog();
    const start = screen.getByRole("button", { name: "Start run" });
    expect((start as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(screen.getByLabelText("Prompt"), { target: { value: "   " } });
    expect((start as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(screen.getByLabelText("Prompt"), { target: { value: "check the disk" } });
    expect((start as HTMLButtonElement).disabled).toBe(false);
  });

  it("posts the trimmed prompt and navigates to the new run", async () => {
    const fetchMock = mockFetch(() => json({ id: "run-42", status: "running" }, 202));
    renderDialog();

    fireEvent.change(screen.getByLabelText("Prompt"), { target: { value: "  check the disk  " } });
    fireEvent.click(screen.getByRole("button", { name: "Start run" }));

    await waitFor(() => {
      expect(screen.getByText("detail page")).toBeTruthy();
    });
    expect(screen.getByTestId("location").textContent).toBe("/runs/run-42");
    expect(JSON.parse(bodyOf(fetchMock.mock.calls[0]?.[1]))).toEqual({
      prompt: "check the disk",
    });
  });

  it("shows the server's error and stays open", async () => {
    mockFetch(() => json({ error: "prompt is required" }, 400));
    renderDialog();

    fireEvent.change(screen.getByLabelText("Prompt"), { target: { value: "x" } });
    fireEvent.click(screen.getByRole("button", { name: "Start run" }));

    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByText("prompt is required")).toBeTruthy();
    expect(screen.getByRole("dialog")).toBeTruthy();
  });

  it("closes on Escape and on Cancel", () => {
    const onClose = renderDialog();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onClose).toHaveBeenCalledTimes(2);
  });
});
