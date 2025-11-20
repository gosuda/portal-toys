package main

import (
	"errors"
	"sync"
)

var (
	errAlreadyJoined = errors.New("player already joined another room")
)

// RoomManager keeps global room registry similar to mafiaList in the JS version.
type RoomManager struct {
	mu      sync.RWMutex
	rooms   map[string]*Room
	players map[string]*Room
}

func NewRoomManager() *RoomManager {
	return &RoomManager{
		rooms:   make(map[string]*Room),
		players: make(map[string]*Room),
	}
}

func (m *RoomManager) Attach(roomName string, c *Client) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.players[c.name]; ok {
		return errAlreadyJoined
	}
	room, ok := m.rooms[roomName]
	if !ok {
		room = NewRoom(roomName, m)
		m.rooms[roomName] = room
	}
	m.players[c.name] = room
	room.enqueue(func(r *Room) {
		r.addPlayer(c)
	})
	return nil
}

func (m *RoomManager) Detach(c *Client) {
	m.mu.Lock()
	room, ok := m.players[c.name]
	if ok {
		delete(m.players, c.name)
	}
	m.mu.Unlock()
	if ok {
		room.enqueue(func(r *Room) {
			r.removePlayer(c)
		})
	}
}

func (m *RoomManager) RouteMessage(c *Client, msg ClientMessage) {
	m.mu.RLock()
	room := m.players[c.name]
	m.mu.RUnlock()
	if room == nil {
		c.pushSystem("참여 중인 방이 없습니다.")
		return
	}
	room.enqueue(func(r *Room) {
		r.handleMessage(c, msg)
	})
}

func (m *RoomManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, room := range m.rooms {
		room.close()
		delete(m.rooms, name)
	}
	m.players = make(map[string]*Room)
}

func (m *RoomManager) removeRoom(name string, room *Room) {
	m.mu.Lock()
	if current, ok := m.rooms[name]; ok && current == room {
		delete(m.rooms, name)
	}
	m.mu.Unlock()
}
