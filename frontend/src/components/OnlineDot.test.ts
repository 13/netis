import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import OnlineDot from "./OnlineDot.vue";

describe("OnlineDot", () => {
  it("shows 'online' with a green dot for a recent timestamp", () => {
    const wrapper = mount(OnlineDot, {
      props: { lastSeen: new Date().toISOString(), showLabel: true },
    });
    expect(wrapper.text()).toContain("online");
    expect(wrapper.html()).toContain("bg-status-free");
  });

  it("shows a relative time once past the online window", () => {
    const old = new Date(Date.now() - 30 * 60 * 1000).toISOString();
    const wrapper = mount(OnlineDot, { props: { lastSeen: old, showLabel: true } });
    expect(wrapper.text()).toContain("30m ago");
  });

  it("renders no label text when showLabel is false", () => {
    const wrapper = mount(OnlineDot, { props: { lastSeen: null } });
    expect(wrapper.text()).toBe("");
  });
});
