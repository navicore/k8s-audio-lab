package main

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AudioChunk represents a chunk of audio with metadata
type AudioChunk struct {
	IntervalID   string            `json:"interval_id"`
	LoopCount    int               `json:"loop_count"`
	Position     int               `json:"position"`
	TotalChunks  int               `json:"total_chunks"`
	Timestamp    int64             `json:"timestamp"`
	Audio        string            `json:"audio"` // hex encoded
	SampleRate   int               `json:"sample_rate"`
	Channels     int               `json:"channels"`
	SampleWidth  int               `json:"sample_width"`
	AudioFormat  map[string]int    `json:"audio_format"`
}

// AudioServer manages the audio loop and clients
type AudioServer struct {
	wavFile         string
	chunkDurationMs int
	audioChunks     [][]byte
	currentPosition int
	loopStartTime   time.Time
	intervalID      string
	loopCount       int
	sampleRate      int
	channels        int
	sampleWidth     int
	
	listeners    map[chan AudioChunk]bool
	listenersMux sync.RWMutex
	
	totalDurationMs int
}

// NewAudioServer creates a new audio server instance
func NewAudioServer(wavFile string, chunkDurationMs int) *AudioServer {
	return &AudioServer{
		wavFile:         wavFile,
		chunkDurationMs: chunkDurationMs,
		listeners:       make(map[chan AudioChunk]bool),
	}
}

// LoadAudio loads and chunks the WAV file
func (s *AudioServer) LoadAudio() error {
	file, err := os.Open(s.wavFile)
	if err != nil {
		return fmt.Errorf("failed to open WAV file: %w", err)
	}
	defer file.Close()

	// Read RIFF header
	var riffHeader struct {
		ChunkID   [4]byte
		ChunkSize uint32
		Format    [4]byte
	}
	
	if err := binary.Read(file, binary.LittleEndian, &riffHeader); err != nil {
		return fmt.Errorf("failed to read RIFF header: %w", err)
	}
	
	if string(riffHeader.ChunkID[:]) != "RIFF" || string(riffHeader.Format[:]) != "WAVE" {
		return fmt.Errorf("not a valid WAV file")
	}

	// Process chunks to find fmt and data
	var formatInfo struct {
		AudioFormat   uint16
		NumChannels   uint16
		SampleRate    uint32
		ByteRate      uint32
		BlockAlign    uint16
		BitsPerSample uint16
	}
	
	foundFormat := false
	
	// Look for chunks
	for {
		var chunkID [4]byte
		var chunkSize uint32
		
		if err := binary.Read(file, binary.LittleEndian, &chunkID); err != nil {
			if err == io.EOF {
				return fmt.Errorf("data chunk not found")
			}
			return fmt.Errorf("failed to read chunk ID: %w", err)
		}
		if err := binary.Read(file, binary.LittleEndian, &chunkSize); err != nil {
			return fmt.Errorf("failed to read chunk size: %w", err)
		}
		
		chunkIDStr := string(chunkID[:])
		
		if chunkIDStr == "fmt " {
			// Read format info
			if err := binary.Read(file, binary.LittleEndian, &formatInfo); err != nil {
				return fmt.Errorf("failed to read format info: %w", err)
			}
			foundFormat = true
			
			// Skip any extra format bytes
			extraBytes := int(chunkSize) - 16
			if extraBytes > 0 {
				file.Seek(int64(extraBytes), 1)
			}
		} else if chunkIDStr == "data" && foundFormat {
			// Found data chunk
			s.sampleRate = int(formatInfo.SampleRate)
			s.channels = int(formatInfo.NumChannels)
			s.sampleWidth = int(formatInfo.BitsPerSample / 8)
			
			// Read all audio data
			audioData := make([]byte, chunkSize)
			if _, err := io.ReadFull(file, audioData); err != nil {
				return fmt.Errorf("failed to read audio data: %w", err)
			}
			
			// Calculate chunk size
			bytesPerMs := (s.sampleRate * s.sampleWidth * s.channels) / 1000
			chunkSize := bytesPerMs * s.chunkDurationMs
			
			// Ensure even chunk size for 16-bit audio
			if chunkSize%2 != 0 {
				chunkSize++
			}
			
			// Split into chunks
			s.audioChunks = nil
			for i := 0; i < len(audioData); i += chunkSize {
				end := i + chunkSize
				if end > len(audioData) {
					end = len(audioData)
				}
				
				chunk := audioData[i:end]
				if len(chunk) == chunkSize {
					s.audioChunks = append(s.audioChunks, chunk)
				} else if len(chunk) > 0 {
					// Pad last chunk
					padded := make([]byte, chunkSize)
					copy(padded, chunk)
					s.audioChunks = append(s.audioChunks, padded)
				}
			}
			
			s.totalDurationMs = len(s.audioChunks) * s.chunkDurationMs
			
			log.Printf("Loaded audio: %d channels, %d Hz, %d-bit, %d chunks, %dms total",
				s.channels, s.sampleRate, s.sampleWidth*8, len(s.audioChunks), s.totalDurationMs)
			
			return nil
		} else {
			// Skip unknown chunks
			if _, err := file.Seek(int64(chunkSize), 1); err != nil {
				return fmt.Errorf("failed to skip chunk %s: %w", chunkIDStr, err)
			}
		}
	}
}

// Start begins the audio loop
func (s *AudioServer) Start() {
	go s.audioLoop()
}

// audioLoop continuously plays audio chunks
func (s *AudioServer) audioLoop() {
	time.Sleep(time.Second) // Give server time to start
	
	ticker := time.NewTicker(time.Duration(s.chunkDurationMs) * time.Millisecond)
	defer ticker.Stop()
	
	for range ticker.C {
		// Start of new loop
		if s.currentPosition == 0 {
			s.intervalID = uuid.New().String()
			s.loopStartTime = time.Now()
			s.loopCount++
			log.Printf("Starting loop #%d, interval: %s", s.loopCount, s.intervalID)
		}
		
		// Create chunk data
		chunk := AudioChunk{
			IntervalID:  s.intervalID,
			LoopCount:   s.loopCount,
			Position:    s.currentPosition,
			TotalChunks: len(s.audioChunks),
			Timestamp:   time.Now().UnixMilli(),
			Audio:       hex.EncodeToString(s.audioChunks[s.currentPosition]),
			SampleRate:  s.sampleRate,
			Channels:    s.channels,
			SampleWidth: s.sampleWidth,
			AudioFormat: map[string]int{
				"channels":        s.channels,
				"sample_rate":     s.sampleRate,
				"bits_per_sample": s.sampleWidth * 8,
			},
		}
		
		// Send to all listeners
		s.broadcast(chunk)
		
		// Move to next position
		s.currentPosition = (s.currentPosition + 1) % len(s.audioChunks)
	}
}

// broadcast sends chunk to all listeners
func (s *AudioServer) broadcast(chunk AudioChunk) {
	s.listenersMux.RLock()
	defer s.listenersMux.RUnlock()
	
	for ch := range s.listeners {
		select {
		case ch <- chunk:
		default:
			// Channel full, skip
		}
	}
}

// AddListener adds a new listener channel
func (s *AudioServer) AddListener(ch chan AudioChunk) {
	s.listenersMux.Lock()
	defer s.listenersMux.Unlock()
	s.listeners[ch] = true
	log.Printf("Client connected. Total listeners: %d", len(s.listeners))
}

// RemoveListener removes a listener channel
func (s *AudioServer) RemoveListener(ch chan AudioChunk) {
	s.listenersMux.Lock()
	defer s.listenersMux.Unlock()
	delete(s.listeners, ch)
	close(ch)
	log.Printf("Client disconnected. Total listeners: %d", len(s.listeners))
}

// GetState returns current server state
func (s *AudioServer) GetState() map[string]interface{} {
	elapsedMs := 0
	if !s.loopStartTime.IsZero() {
		elapsedMs = int(time.Since(s.loopStartTime).Milliseconds())
	}
	
	return map[string]interface{}{
		"interval_id":      s.intervalID,
		"loop_count":       s.loopCount,
		"current_position": s.currentPosition,
		"total_chunks":     len(s.audioChunks),
		"elapsed_ms":       elapsedMs,
		"total_duration_ms": s.totalDurationMs,
		"chunk_duration_ms": s.chunkDurationMs,
		"audio_format": map[string]int{
			"channels":        s.channels,
			"sample_rate":     s.sampleRate,
			"bits_per_sample": s.sampleWidth * 8,
		},
	}
}

var audioServer *AudioServer

// handleIndex serves the web player interface
func handleIndex(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Audio Loop Broadcast</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; background: #f5f5f5; }
        .container { max-width: 600px; margin: 0 auto; background: white; padding: 30px; border-radius: 10px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        h1 { color: #333; }
        button { padding: 12px 24px; font-size: 16px; margin: 10px; border: none; border-radius: 5px; cursor: pointer; }
        button:hover { opacity: 0.9; }
        .play { background: #4CAF50; color: white; }
        .stop { background: #f44336; color: white; }
        #status { margin: 20px 0; padding: 15px; background: #f0f0f0; border-radius: 5px; }
        .metric { margin: 8px 0; display: flex; justify-content: space-between; }
        .metric span { font-weight: bold; }
        #error { color: red; margin: 10px 0; }
    </style>
</head>
<body>
    <div class="container">
        <h1>🎵 Audio Loop Broadcast</h1>
        <p>This server continuously broadcasts audio in a loop. Connect anytime to join the stream!</p>
        <div>
            <button class="play" onclick="startStream()">▶️ Play Stream</button>
            <button class="stop" onclick="stopStream()">⏹️ Stop</button>
        </div>
        <div id="error"></div>
        <div id="status">
            <div class="metric">Status: <span id="state">Disconnected</span></div>
            <div class="metric">Loop Count: <span id="loop">-</span></div>
            <div class="metric">Position: <span id="position">-</span></div>
            <div class="metric">Interval ID: <span id="interval">-</span></div>
            <div class="metric">Audio Format: <span id="format">-</span></div>
        </div>
    </div>
    
    <script>
        let eventSource = null;
        let audioContext = null;
        let nextPlayTime = 0;
        let audioFormat = null;
        let isPlaying = false;
        
        async function startStream() {
            if (eventSource) return;
            
            try {
                audioContext = new (window.AudioContext || window.webkitAudioContext)();
                nextPlayTime = audioContext.currentTime + 0.1;
                isPlaying = true;
                
                eventSource = new EventSource('/stream');
                document.getElementById('state').textContent = 'Connecting...';
                document.getElementById('error').textContent = '';
                
                eventSource.onmessage = (event) => {
                    const data = JSON.parse(event.data);
                    
                    if (!audioFormat && data.audio_format) {
                        audioFormat = data.audio_format;
                        document.getElementById('format').textContent = 
                            audioFormat.sample_rate + 'Hz, ' + audioFormat.bits_per_sample + '-bit, ' + audioFormat.channels + 'ch';
                    }
                    
                    document.getElementById('state').textContent = 'Connected';
                    document.getElementById('loop').textContent = data.loop_count;
                    document.getElementById('position').textContent = data.position + '/' + data.total_chunks;
                    document.getElementById('interval').textContent = 
                        data.interval_id ? data.interval_id.substring(0, 8) + '...' : '-';
                    
                    if (data.audio && isPlaying) {
                        playChunk(data);
                    }
                };
                
                eventSource.onerror = (e) => {
                    document.getElementById('state').textContent = 'Error';
                    document.getElementById('error').textContent = 'Connection lost. Click Play to reconnect.';
                    stopStream();
                };
            } catch (e) {
                document.getElementById('error').textContent = 'Error: ' + e.message;
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
                document.getElementById('error').textContent = 'Playback error: ' + e.message;
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
            audioFormat = null;
        }
    </script>
</body>
</html>`
	
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// handleStream handles SSE streaming
func handleStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	ch := make(chan AudioChunk, 10)
	audioServer.AddListener(ch)
	defer audioServer.RemoveListener(ch)
	
	// Send initial state
	state := audioServer.GetState()
	if data, err := json.Marshal(state); err == nil {
		fmt.Fprintf(w, "data: %s\n\n", data)
		w.(http.Flusher).Flush()
	}
	
	// Stream chunks
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

// handleStatus returns server status
func handleStatus(w http.ResponseWriter, r *http.Request) {
	state := audioServer.GetState()
	audioServer.listenersMux.RLock()
	state["listeners"] = len(audioServer.listeners)
	audioServer.listenersMux.RUnlock()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

func main() {
	// Create audio server
	audioServer = NewAudioServer("/app/audio.wav", 100) // 100ms chunks
	
	// Load audio
	if err := audioServer.LoadAudio(); err != nil {
		log.Fatalf("Failed to load audio: %v", err)
	}
	
	// Start audio loop
	audioServer.Start()
	
	// Setup HTTP routes
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/stream", handleStream)
	http.HandleFunc("/status", handleStatus)
	
	// Start HTTP server
	log.Println("Audio source server started on :8000")
	if err := http.ListenAndServe(":8000", nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}