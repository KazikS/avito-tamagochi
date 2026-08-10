import { useEffect } from 'react';
import { useAppDispatch } from '@/app/store';
import { petApi } from './api';

// Сокет ничего не считает (docs/openapi.json → x-websocket.description) —
// сервер уже посчитал новое состояние и просто уведомляет. Поэтому здесь
// не разбирается payload события и не мёржатся три разных формы
// (pet.stats/xp.gained/level.up) вручную: любое сообщение — сигнал
// перечитать /pet заново, единственный источник истины остаётся REST.
//
// Реконнект — простой фиксированный таймаут, не экспоненциальный бэкофф:
// на хакатон-демо важнее быстро восстановить соединение после сна вкладки,
// чем беречь единичный сервер от единичного клиента. Контракт (правило
// «если сокет не поднялся три раза подряд — поллинг GET /pet раз в 20с»)
// в этом срезе не реализован осознанно: минимальный фронт полагается на
// реконнект, а не на резервный поллинг.
const RECONNECT_DELAY_MS = 2000;

export function useLiveUpdates(): void {
  const dispatch = useAppDispatch();

  useEffect(() => {
    let socket: WebSocket | undefined;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;
    let stopped = false;

    const connect = () => {
      const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
      socket = new WebSocket(`${protocol}://${window.location.host}/ws`);

      socket.onmessage = () => {
        // Rewards тоже: xp.gained/level.up могли открыть новую награду —
        // тот же довод, что в features/pet/api.ts → act.
        dispatch(petApi.util.invalidateTags(['Pet', 'Rewards']));
      };
      socket.onclose = () => {
        if (stopped) return;
        retryTimer = setTimeout(connect, RECONNECT_DELAY_MS);
      };
    };

    connect();

    return () => {
      stopped = true;
      clearTimeout(retryTimer);
      socket?.close();
    };
  }, [dispatch]);
}
