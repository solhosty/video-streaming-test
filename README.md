# Video Streaming Platform

A Go-based video streaming platform with WebRTC support for live streaming from your MacBook to viewers.

## Features

- Live video streaming using WebRTC
- Real-time signaling server built with Go and WebSockets
- Web-based streamer and viewer interfaces
- Multiple concurrent streams support
- STUN server integration for NAT traversal

## Setup Instructions

### Prerequisites

- Go 1.21 or higher
- Modern web browser with WebRTC support
- Camera and microphone access

### Installation

1. **Install Go** (if not already installed):
   - macOS: `brew install go` or download from https://golang.org/dl/
   - Verify installation: `go version`

2. Install project dependencies:
   ```bash
   go mod tidy
   ```

3. Start the server:
   ```bash
   go run main.go
   ```

4. Open your browser and navigate to:
   ```
   http://localhost:8080
   ```

## Usage

### To Start Streaming:
1. Click "Start Streaming" in the Streamer section
2. Grant camera/microphone permissions when prompted
3. Your stream will be available to viewers

### To Watch Streams:
1. Available streams will appear in the "Available Streams" list
2. Click "Watch" next to any stream to start viewing
3. The video will appear in the viewer section

## Technical Details

- **Backend**: Go with Gorilla WebSocket for signaling
- **Frontend**: Vanilla JavaScript with WebRTC APIs
- **Signaling**: WebSocket-based peer-to-peer connection establishment
- **Video/Audio**: Direct WebRTC peer connections
- **STUN Server**: Google's public STUN server for NAT traversal

## Local Testing

The application is configured to work locally by default. Both the streamer and viewer functionality can be tested on the same machine by opening multiple browser tabs.

## Browser Compatibility

- Chrome/Chromium 80+
- Firefox 75+
- Safari 14+
- Edge 80+

## Security Note

This implementation is for local development only. For production use, implement proper authentication, HTTPS, and TURN servers for better connectivity.# video-streaming-test
