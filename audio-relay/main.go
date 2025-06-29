package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// BufferEntry represents a buffered audio chunk with timing info
type BufferEntry struct {
	Data         interface{}
	ReceivedTime time.Time
	RelativeTime float64
}

// AudioBuffer is a ring buffer for audio chunks
type AudioBuffer struct {
	maxSize   int
	buffer    []BufferEntry
	startTime *time.Time
	mu        sync.RWMutex
}

// NewAudioBuffer creates a new audio buffer
func NewAudioBuffer(maxSeconds int) *AudioBuffer {
	return &AudioBuffer{
		maxSize: maxSeconds * 10, // Assuming 100ms chunks
		buffer:  make([]BufferEntry, 0),
	}
}

// AddChunk adds a chunk to the buffer
func (b *AudioBuffer) AddChunk(chunkData interface{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	now := time.Now()
	if b.startTime == nil {
		b.startTime = &now
	}
	
	entry := BufferEntry{
		Data:         chunkData,
		ReceivedTime: now,
		RelativeTime: now.Sub(*b.startTime).Seconds(),
	}
	
	b.buffer = append(b.buffer, entry)
	if len(b.buffer) > b.maxSize {
		b.buffer = b.buffer[1:]
	}
}

// GetChunkAtDelay returns the chunk that should play now given the delay
func (b *AudioBuffer) GetChunkAtDelay(delaySeconds float64) interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()
	
	if len(b.buffer) == 0 || b.startTime == nil {
		return nil
	}
	
	// Special case: zero delay means play the most recent chunk
	if delaySeconds == 0 {
		return b.buffer[len(b.buffer)-1].Data
	}
	
	currentRelativeTime := time.Since(*b.startTime).Seconds()
	targetTime := currentRelativeTime - delaySeconds
	
	// Find the chunk closest to our target time
	for _, entry := range b.buffer {
		if entry.RelativeTime >= targetTime {
			return entry.Data
		}
	}
	
	return nil
}

// GetStats returns buffer statistics
func (b *AudioBuffer) GetStats() map[string]interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()
	
	if len(b.buffer) == 0 {
		return map[string]interface{}{"size": 0, "duration": 0}
	}
	
	duration := 0.0
	if len(b.buffer) > 1 {
		duration = b.buffer[len(b.buffer)-1].RelativeTime - b.buffer[0].RelativeTime
	}
	
	oldestAge := 0.0
	if len(b.buffer) > 0 {
		oldestAge = time.Since(b.buffer[0].ReceivedTime).Seconds()
	}
	
	return map[string]interface{}{
		"size":       len(b.buffer),
		"duration":   duration,
		"oldest_age": oldestAge,
	}
}

// ClientInfo represents a connected client
type ClientInfo struct {
	Queue    chan map[string]interface{}
	DelayMs  int
}

// AudioRelay manages the relay service
type AudioRelay struct {
	sourceURL      string
	buffer         *AudioBuffer
	listeners      map[int]*ClientInfo
	listenersMux   sync.RWMutex
	currentState   map[string]interface{}
	isConnected    bool
	relayID        string
	clientCounter  int
	latestChunk    interface{}
}

// NewAudioRelay creates a new relay instance
func NewAudioRelay() *AudioRelay {
	sourceURL := os.Getenv("AUDIO_SOURCE_URL")
	if sourceURL == "" {
		sourceURL = "http://audio-source:8000"
	}
	
	return &AudioRelay{
		sourceURL:    sourceURL,
		buffer:       NewAudioBuffer(20),
		listeners:    make(map[int]*ClientInfo),
		currentState: make(map[string]interface{}),
		relayID:      "relay-buffered",
	}
}

// ConnectToSource connects to the audio source and buffers chunks
func (r *AudioRelay) ConnectToSource(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		
		log.Printf("Connecting to audio source at %s/stream", r.sourceURL)
		
		req, err := http.NewRequestWithContext(ctx, "GET", r.sourceURL+"/stream", nil)
		if err != nil {
			log.Printf("Failed to create request: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("Connection to source failed: %v", err)
			r.isConnected = false
			time.Sleep(5 * time.Second)
			continue
		}
		
		r.isConnected = true
		log.Println("Connected to audio source")
		
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if len(line) > 6 && line[:6] == "data: " {
				var data map[string]interface{}
				if err := json.Unmarshal([]byte(line[6:]), &data); err == nil {
					// Update current state
					r.currentState = map[string]interface{}{
						"source_interval_id": data["interval_id"],
						"source_loop_count":  data["loop_count"],
						"source_position":    data["position"],
						"total_chunks":       data["total_chunks"],
						"audio_format":       data["audio_format"],
					}
					
					// Buffer the chunk
					r.buffer.AddChunk(data)
					
					// Store latest chunk for real-time playback
					r.latestChunk = data
					
					// Send immediately to real-time clients
					r.sendToRealtimeClients(data)
				}
			}
		}
		
		resp.Body.Close()
		r.isConnected = false
		log.Println("Disconnected from source")
		time.Sleep(5 * time.Second)
	}
}

// sendToRealtimeClients sends chunk immediately to real-time (0 delay) clients
func (r *AudioRelay) sendToRealtimeClients(chunkData interface{}) {
	r.listenersMux.RLock()
	defer r.listenersMux.RUnlock()
	
	chunk := chunkData.(map[string]interface{})
	now := time.Now().UnixMilli()
	
	for clientID, clientInfo := range r.listeners {
		if clientInfo.DelayMs == 0 {
			relayData := make(map[string]interface{})
			for k, v := range chunk {
				relayData[k] = v
			}
			relayData["relay_id"] = r.relayID
			relayData["relay_timestamp"] = now
			relayData["source_timestamp"] = chunk["timestamp"]
			relayData["configured_delay_ms"] = 0
			
			if sourceTs, ok := chunk["timestamp"].(float64); ok {
				relayData["actual_delay_ms"] = now - int64(sourceTs)
			}
			
			relayData["buffer_stats"] = r.buffer.GetStats()
			
			select {
			case clientInfo.Queue <- relayData:
			default:
				log.Printf("Queue full for real-time client %d", clientID)
			}
		}
	}
}

// PlaybackLoop sends buffered audio to clients based on their delay settings
func (r *AudioRelay) PlaybackLoop(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.listenersMux.RLock()
			clients := make(map[int]*ClientInfo)
			for id, info := range r.listeners {
				clients[id] = info
			}
			r.listenersMux.RUnlock()
			
			for clientID, clientInfo := range clients {
				if clientInfo.DelayMs > 0 { // Skip real-time clients
					delaySeconds := float64(clientInfo.DelayMs) / 1000.0
					chunkData := r.buffer.GetChunkAtDelay(delaySeconds)
					
					if chunkData != nil {
						chunk := chunkData.(map[string]interface{})
						relayData := make(map[string]interface{})
						for k, v := range chunk {
							relayData[k] = v
						}
						
						now := time.Now().UnixMilli()
						relayData["relay_id"] = r.relayID
						relayData["relay_timestamp"] = now
						relayData["source_timestamp"] = chunk["timestamp"]
						relayData["configured_delay_ms"] = clientInfo.DelayMs
						
						if sourceTs, ok := chunk["timestamp"].(float64); ok {
							relayData["actual_delay_ms"] = now - int64(sourceTs)
						}
						
						relayData["buffer_stats"] = r.buffer.GetStats()
						
						select {
						case clientInfo.Queue <- relayData:
						default:
							log.Printf("Queue full for client %d", clientID)
						}
					}
				}
			}
		}
	}
}

// AddClient adds a new client
func (r *AudioRelay) AddClient(delayMs int) (int, chan map[string]interface{}) {
	r.listenersMux.Lock()
	defer r.listenersMux.Unlock()
	
	clientID := r.clientCounter
	r.clientCounter++
	
	ch := make(chan map[string]interface{}, 10)
	r.listeners[clientID] = &ClientInfo{
		Queue:   ch,
		DelayMs: delayMs,
	}
	
	log.Printf("Client %d connected with %dms delay. Total: %d", clientID, delayMs, len(r.listeners))
	return clientID, ch
}

// RemoveClient removes a client
func (r *AudioRelay) RemoveClient(clientID int) {
	r.listenersMux.Lock()
	defer r.listenersMux.Unlock()
	
	if info, ok := r.listeners[clientID]; ok {
		close(info.Queue)
		delete(r.listeners, clientID)
		log.Printf("Client %d disconnected. Total: %d", clientID, len(r.listeners))
	}
}

// UpdateClientDelay updates the delay for a client
func (r *AudioRelay) UpdateClientDelay(clientID, delayMs int) {
	r.listenersMux.Lock()
	defer r.listenersMux.Unlock()
	
	if info, ok := r.listeners[clientID]; ok {
		info.DelayMs = delayMs
		log.Printf("Updated client %d delay to %dms", clientID, delayMs)
	}
}

var relay *AudioRelay

// handleIndex serves the relay web interface
func handleIndex(w http.ResponseWriter, r *http.Request) {
	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <title>Audio Relay with Buffer</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; background: #f5f5f5; }
        .container { max-width: 700px; margin: 0 auto; background: white; padding: 30px; border-radius: 10px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        h1 { color: #333; }
        .controls { background: #e3f2fd; padding: 20px; border-radius: 5px; margin: 20px 0; }
        .slider-container { margin: 20px 0; }
        .slider { width: 100%%; height: 40px; -webkit-appearance: none; appearance: none; background: #ddd; outline: none; opacity: 0.7; transition: opacity 0.2s; border-radius: 5px; }
        .slider:hover { opacity: 1; }
        .slider::-webkit-slider-thumb { -webkit-appearance: none; appearance: none; width: 25px; height: 40px; background: #2196F3; cursor: pointer; border-radius: 5px; }
        .slider::-moz-range-thumb { width: 25px; height: 40px; background: #2196F3; cursor: pointer; border-radius: 5px; }
        .delay-display { font-size: 24px; font-weight: bold; color: #2196F3; text-align: center; margin: 10px 0; }
        button { padding: 12px 24px; font-size: 16px; margin: 10px; border: none; border-radius: 5px; cursor: pointer; }
        button:hover { opacity: 0.9; }
        .play { background: #4CAF50; color: white; }
        .stop { background: #f44336; color: white; }
        #status { margin: 20px 0; padding: 15px; background: #f0f0f0; border-radius: 5px; }
        .metric { margin: 8px 0; display: flex; justify-content: space-between; }
        .metric span { font-weight: bold; }
        .buffer-stats { background: #fff3cd; padding: 10px; border-radius: 5px; margin: 10px 0; }
    </style>
</head>
<body>
    <div class="container">
        <h1>🔄 Audio Relay with Variable Buffer</h1>
        
        <div class="controls">
            <h3>Latency Control</h3>
            <div class="slider-container">
                <input type="range" min="0" max="15000" value="2000" step="100" class="slider" id="latencySlider">
                <div class="delay-display" id="delayDisplay">2.0 seconds</div>
            </div>
            <div style="display: flex; justify-content: space-between; color: #666;">
                <span>0s</span>
                <span>Real-time ← → Delayed</span>
                <span>15s</span>
            </div>
        </div>
        
        <div>
            <button class="play" onclick="startStream()">▶️ Play Stream</button>
            <button class="stop" onclick="stopStream()">⏹️ Stop</button>
        </div>
        
        <div class="buffer-stats" id="bufferStats">
            <strong>Buffer:</strong> <span id="bufferInfo">Not connected</span>
        </div>
        
        <div id="status">
            <div class="metric">Status: <span id="state">Disconnected</span></div>
            <div class="metric">Source Connected: <span id="source-connected">%v</span></div>
            <div class="metric">Loop Count: <span id="loop">-</span></div>
            <div class="metric">Position: <span id="position">-</span></div>
            <div class="metric">Actual Latency: <span id="actualLatency">-</span></div>
        </div>
    </div>
    
    <script>
        let eventSource = null;
        let audioContext = null;
        let nextPlayTime = 0;
        let isPlaying = false;
        let currentDelay = 2000;
        let clientId = null;
        
        const slider = document.getElementById('latencySlider');
        const delayDisplay = document.getElementById('delayDisplay');
        
        slider.oninput = function() {
            currentDelay = parseInt(this.value);
            const seconds = (currentDelay / 1000).toFixed(1);
            delayDisplay.textContent = seconds + ' seconds';
            
            if (eventSource && eventSource.readyState === EventSource.OPEN && clientId !== null) {
                fetch('/set-delay', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ client_id: clientId, delay_ms: currentDelay })
                });
            }
        }
        
        async function startStream() {
            if (eventSource) return;
            
            try {
                audioContext = new (window.AudioContext || window.webkitAudioContext)();
                nextPlayTime = audioContext.currentTime + 0.1;
                isPlaying = true;
                
                eventSource = new EventSource('/stream?delay=' + currentDelay);
                document.getElementById('state').textContent = 'Connecting...';
                
                eventSource.onmessage = (event) => {
                    const data = JSON.parse(event.data);
                    
                    if (data.client_id !== undefined) {
                        clientId = data.client_id;
                    }
                    
                    document.getElementById('state').textContent = 'Connected';
                    document.getElementById('loop').textContent = data.loop_count || '-';
                    document.getElementById('position').textContent = 
                        data.position !== undefined ? data.position + '/' + data.total_chunks : '-';
                    
                    if (data.actual_delay_ms !== undefined) {
                        const actualSeconds = (data.actual_delay_ms / 1000).toFixed(1);
                        document.getElementById('actualLatency').textContent = actualSeconds + 's';
                    }
                    
                    if (data.buffer_stats) {
                        const stats = data.buffer_stats;
                        document.getElementById('bufferInfo').textContent = 
                            stats.size + ' chunks, ' + stats.duration.toFixed(1) + 's buffered';
                    }
                    
                    if (data.audio && isPlaying) {
                        playChunk(data);
                    }
                };
                
                eventSource.onerror = () => {
                    document.getElementById('state').textContent = 'Error';
                    stopStream();
                };
            } catch (e) {
                console.error('Error:', e);
                stopStream();
            }
        }
        
        function playChunk(data) {
            try {
                const bytes = new Uint8Array(data.audio.match(/.{1,2}/g).map(byte => parseInt(byte, 16)));
                const sampleRate = data.sample_rate || 44100;
                const channels = data.channels || 1;
                const sampleWidth = data.sample_width || 2;
                const samplesPerChannel = bytes.length / (channels * sampleWidth);
                
                const buffer = audioContext.createBuffer(channels, samplesPerChannel, sampleRate);
                
                if (sampleWidth === 2) {
                    for (let channel = 0; channel < channels; channel++) {
                        const channelData = buffer.getChannelData(channel);
                        for (let i = 0; i < samplesPerChannel; i++) {
                            const byteIndex = (i * channels + channel) * 2;
                            const int16 = (bytes[byteIndex + 1] << 8) | bytes[byteIndex];
                            channelData[i] = (int16 > 32767 ? int16 - 65536 : int16) / 32768.0;
                        }
                    }
                }
                
                const source = audioContext.createBufferSource();
                source.buffer = buffer;
                source.connect(audioContext.destination);
                
                const now = audioContext.currentTime;
                if (nextPlayTime < now) {
                    nextPlayTime = now + 0.01;
                }
                source.start(nextPlayTime);
                nextPlayTime += buffer.duration;
                
            } catch (e) {
                console.error('Playback error:', e);
            }
        }
        
        function stopStream() {
            isPlaying = false;
            if (eventSource) {
                eventSource.close();
                eventSource = null;
            }
            if (audioContext) {
                audioContext.close();
                audioContext = null;
            }
            document.getElementById('state').textContent = 'Disconnected';
            document.getElementById('bufferInfo').textContent = 'Not connected';
            clientId = null;
        }
    </script>
</body>
</html>`, relay.isConnected)
	
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// handleStream handles SSE streaming
func handleStream(w http.ResponseWriter, r *http.Request) {
	// Get requested delay
	delayMs := 2000
	if d := r.URL.Query().Get("delay"); d != "" {
		fmt.Sscanf(d, "%d", &delayMs)
	}
	if delayMs < 0 {
		delayMs = 0
	}
	if delayMs > 15000 {
		delayMs = 15000
	}
	
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	
	clientID, ch := relay.AddClient(delayMs)
	defer relay.RemoveClient(clientID)
	
	// Send client ID
	fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"client_id":%d}`, clientID))
	w.(http.Flusher).Flush()
	
	for {
		select {
		case chunk := <-ch:
			if data, err := json.Marshal(chunk); err == nil {
				fmt.Fprintf(w, "data: %s\n\n", data)
				w.(http.Flusher).Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

// handleSetDelay updates delay for a client
func handleSetDelay(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientID int `json:"client_id"`
		DelayMs  int `json:"delay_ms"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	
	if req.DelayMs < 0 {
		req.DelayMs = 0
	}
	if req.DelayMs > 15000 {
		req.DelayMs = 15000
	}
	
	relay.UpdateClientDelay(req.ClientID, req.DelayMs)
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "ok",
		"delay_ms": req.DelayMs,
	})
}

// handleStatus returns server status
func handleStatus(w http.ResponseWriter, r *http.Request) {
	relay.listenersMux.RLock()
	numListeners := len(relay.listeners)
	relay.listenersMux.RUnlock()
	
	status := map[string]interface{}{
		"relay_id":      relay.relayID,
		"source_url":    relay.sourceURL,
		"is_connected":  relay.isConnected,
		"listeners":     numListeners,
		"buffer_stats":  relay.buffer.GetStats(),
		"current_state": relay.currentState,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func main() {
	relay = NewAudioRelay()
	
	ctx := context.Background()
	
	// Start background tasks
	go relay.ConnectToSource(ctx)
	go relay.PlaybackLoop(ctx)
	
	// Setup HTTP routes
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/stream", handleStream)
	http.HandleFunc("/set-delay", handleSetDelay)
	http.HandleFunc("/status", handleStatus)
	
	// Start HTTP server
	log.Println("Audio relay server started on :8001")
	if err := http.ListenAndServe(":8001", nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}