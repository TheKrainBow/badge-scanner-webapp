import { createContext, useCallback, useContext, useEffect, useRef, type ReactNode } from "react";

// Tagged union matching the JSON envelopes internal/wshub broadcasts.
export type LookupEvent = {
  type: "lookup";
  keyName: string;
  uidHex: string;
  login: string;
  coalitionName: string;
  coalitionColor: string;
  found: boolean;
  timestamp: number;
};
export type ProgressEvent = {
  type: "progress";
  job: string;
  current: number;
  total: number;
  currentItem: string;
};
export type RefreshCompleteEvent = { type: "refreshComplete"; job: string; count: number };
export type RefreshErrorEvent = { type: "refreshError"; job: string; message: string };
export type WSEvent = LookupEvent | ProgressEvent | RefreshCompleteEvent | RefreshErrorEvent;

type Handler<T extends WSEvent> = (event: T) => void;

interface EventsState {
  subscribe<T extends WSEvent["type"]>(type: T, handler: Handler<Extract<WSEvent, { type: T }>>): () => void;
}

const EventsContext = createContext<EventsState | null>(null);

/**
 * Connects once to the backend's live-event feed (GET /api/events, session-
 * cookie authenticated — see backend/internal/api/events_handlers.go for
 * why this doesn't use the X-Client-Id/Secret header gate other routes
 * use) and fans out parsed messages to subscribers by event type.
 * Reconnects automatically if the agent isn't running yet or drops — same
 * shape as the old (now-removed) useReaderAgent hook's reconnect logic.
 */
export function EventsProvider({ children }: { children: ReactNode }) {
  const handlersRef = useRef<Map<string, Set<Handler<any>>>>(new Map());

  useEffect(() => {
    let socket: WebSocket | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let stopped = false;

    function connect() {
      if (stopped) return;
      const proto = location.protocol === "https:" ? "wss:" : "ws:";
      socket = new WebSocket(`${proto}//${location.host}/api/events`);

      socket.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data) as WSEvent;
          const handlers = handlersRef.current.get(data.type);
          handlers?.forEach((h) => h(data));
        } catch {
          // ignore malformed messages
        }
      };
      socket.onclose = () => {
        if (!stopped) reconnectTimer = setTimeout(connect, 2000);
      };
      socket.onerror = () => socket?.close();
    }

    connect();
    return () => {
      stopped = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      socket?.close();
    };
  }, []);

  const subscribe = useCallback(<T extends WSEvent["type"]>(type: T, handler: Handler<Extract<WSEvent, { type: T }>>) => {
    let set = handlersRef.current.get(type);
    if (!set) {
      set = new Set();
      handlersRef.current.set(type, set);
    }
    set.add(handler);
    return () => set!.delete(handler);
  }, []);

  return <EventsContext.Provider value={{ subscribe }}>{children}</EventsContext.Provider>;
}

export function useEvents(): EventsState {
  const ctx = useContext(EventsContext);
  if (!ctx) throw new Error("useEvents must be used within EventsProvider");
  return ctx;
}
