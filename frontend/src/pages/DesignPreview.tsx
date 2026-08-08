import { useState } from 'react';
import { CameraDashboard } from '../components/CameraDashboard';
import type { CameraFeed } from '../components/CameraDashboard';
import { useToast } from '../components/Toast';

const MOCK_CAMERAS: CameraFeed[] = [
  {
    id: '01',
    name: 'Front Door',
    location: 'Entryway',
    status: 'recording',
    resolution: '1920×1080',
    fps: 25,
    lastSeen: new Date(Date.now() - 30_000).toISOString(),
    detections: [
      { class_name: 'person', confidence: 0.94 },
      { class_name: 'car', confidence: 0.71 },
    ],
  },
  {
    id: '02',
    name: 'Backyard',
    location: 'Garden',
    status: 'recording',
    resolution: '2560×1440',
    fps: 20,
    lastSeen: new Date(Date.now() - 90_000).toISOString(),
    detections: [{ class_name: 'dog', confidence: 0.86 }],
  },
  {
    id: '03',
    name: 'Garage',
    location: 'Parking',
    status: 'online',
    resolution: '1920×1080',
    fps: 25,
    lastSeen: new Date(Date.now() - 120_000).toISOString(),
  },
  {
    id: '04',
    name: 'Living Room',
    location: 'Ground floor',
    status: 'online',
    resolution: '1280×720',
    fps: 15,
    lastSeen: new Date(Date.now() - 240_000).toISOString(),
  },
  {
    id: '05',
    name: 'Driveway',
    location: 'North side',
    status: 'online',
    resolution: '1920×1080',
    fps: 25,
    lastSeen: new Date(Date.now() - 300_000).toISOString(),
    detections: [{ class_name: 'person', confidence: 0.62 }],
  },
  {
    id: '06',
    name: 'Kitchen',
    location: 'Ground floor',
    status: 'online',
    resolution: '1280×720',
    fps: 15,
    lastSeen: new Date(Date.now() - 360_000).toISOString(),
  },
  {
    id: '07',
    name: 'Pool Area',
    location: 'Backyard',
    status: 'offline',
    resolution: '2560×1440',
    fps: 20,
  },
  {
    id: '08',
    name: 'Front Gate',
    location: 'Perimeter',
    status: 'offline',
    resolution: '1920×1080',
    fps: 25,
  },
];

export default function DesignPreview() {
  const { toast } = useToast();
  const [armed, setArmed] = useState(true);

  return (
    <CameraDashboard
      siteName="SecureView"
      siteCode="HOME"
      activeNav="cameras"
      cameras={MOCK_CAMERAS}
      armed={armed}
      onCameraSelect={(cam) => toast(`Selected ${cam.name}`)}
      onRecordToggle={(cam, recording) => toast(`${cam.name}: ${recording ? 'recording' : 'stopped'}`)}
      onSnapshot={(cam) => toast(`Snapshot requested: ${cam.name}`)}
      onFullscreen={(cam) => toast(`Fullscreen: ${cam.name}`)}
      onNavSelect={(item) => toast(`Nav: ${item.label}`)}
      onRefresh={() => toast('Refreshing feeds…')}
      onAddCamera={() => toast('Open camera setup wizard')}
      onArmToggle={(next) => {
        setArmed(next);
        toast(next ? 'System armed' : 'System disarmed', next ? 'success' : 'error');
      }}
      onSOS={() => toast('SOS triggered — dispatch notified', 'error')}
    />
  );
}
