// Hand-written declarations for noVNC, which ships no TypeScript types.
// Surface follows docs/API.md for the parts HomeHub uses.

declare module "@novnc/novnc/lib/rfb" {
  export interface RFBCredentials {
    username?: string;
    password?: string;
    target?: string;
  }

  export interface RFBOptions {
    shared?: boolean;
    credentials?: RFBCredentials;
    repeaterID?: string;
    wsProtocols?: string[];
  }

  export interface RFBDisconnectDetail {
    clean: boolean;
  }

  export interface RFBSecurityFailureDetail {
    status: number;
    reason?: string;
  }

  export interface RFBCredentialsRequiredDetail {
    types: string[];
  }

  export interface RFBCapabilities {
    power?: boolean;
  }

  export interface RFBEventMap {
    connect: Event;
    disconnect: CustomEvent<RFBDisconnectDetail>;
    credentialsrequired: CustomEvent<RFBCredentialsRequiredDetail>;
    securityfailure: CustomEvent<RFBSecurityFailureDetail>;
    desktopname: CustomEvent<{ name: string }>;
    clipboard: CustomEvent<{ text: string }>;
    bell: Event;
    capabilities: CustomEvent<{ capabilities: RFBCapabilities }>;
    clippingviewport: CustomEvent<boolean>;
  }

  export default class RFB extends EventTarget {
    constructor(target: HTMLElement, urlOrChannel: string | WebSocket, options?: RFBOptions);

    viewOnly: boolean;
    focusOnClick: boolean;
    clipViewport: boolean;
    dragViewport: boolean;
    scaleViewport: boolean;
    resizeSession: boolean;
    showDotCursor: boolean;
    /** CSS background value for the letterbox area around the remote screen. */
    background: string;
    qualityLevel: number;
    compressionLevel: number;
    readonly capabilities: RFBCapabilities;

    disconnect(): void;
    sendCredentials(credentials: RFBCredentials): void;
    sendKey(keysym: number, code: string | null, down?: boolean): void;
    sendCtrlAltDel(): void;
    focus(options?: FocusOptions): void;
    blur(): void;
    clipboardPasteFrom(text: string): void;
    machineShutdown(): void;
    machineReboot(): void;
    machineReset(): void;

    addEventListener<K extends keyof RFBEventMap>(
      type: K,
      listener: (ev: RFBEventMap[K]) => void,
      options?: boolean | AddEventListenerOptions,
    ): void;
    addEventListener(
      type: string,
      listener: EventListenerOrEventListenerObject | null,
      options?: boolean | AddEventListenerOptions,
    ): void;
    removeEventListener<K extends keyof RFBEventMap>(
      type: K,
      listener: (ev: RFBEventMap[K]) => void,
      options?: boolean | EventListenerOptions,
    ): void;
    removeEventListener(
      type: string,
      listener: EventListenerOrEventListenerObject | null,
      options?: boolean | EventListenerOptions,
    ): void;
  }
}

// noVNC 1.7 exposes RFB only through the package root ("exports": "./core/rfb.js"),
// so the runtime import path is the bare specifier.
declare module "@novnc/novnc" {
  export * from "@novnc/novnc/lib/rfb";
  export { default } from "@novnc/novnc/lib/rfb";
}
