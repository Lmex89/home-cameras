document.addEventListener('DOMContentLoaded', function () {

    // ============ Snapshot Modal ============
    document.querySelectorAll('[data-bs-toggle="modal"][data-snapshot-id]').forEach(el => {
        el.addEventListener('click', function (e) {
            e.preventDefault();
            const id = this.dataset.snapshotId;
            document.getElementById('snapshotModalImage').src = '/api/snapshots/image/' + id;
        });
    });

    // ============ Force Snapshot ============
    document.querySelectorAll('.btn-force-snapshot').forEach(btn => {
        btn.addEventListener('click', async function () {
            const cameraId = this.dataset.cameraId;
            const card = this.closest('.camera-card');
            const alerts = document.getElementById('dashboard-alerts');

            this.disabled = true;
            this.innerHTML = '<span class="spinner-border spinner-border-sm"></span>';

            try {
                const resp = await fetch('/api/cameras/' + cameraId + '/snapshot', { method: 'POST' });
                const data = await resp.json();
                if (data.success) {
                    showAlert(alerts, 'Captura exitosa para cámara ' + cameraId, 'success');
                    setTimeout(() => location.reload(), 1500);
                } else {
                    showAlert(alerts, 'Error: ' + (data.error || 'desconocido'), 'danger');
                }
            } catch (err) {
                showAlert(alerts, 'Error de conexión: ' + err.message, 'danger');
            } finally {
                this.disabled = false;
                this.innerHTML = '<i class="bi bi-camera"></i>';
            }
        });
    });

    // ============ Report ============
    const reportDate = document.getElementById('reportDate');
    const btnLoad = document.getElementById('btnLoadReport');
    const reportContent = document.getElementById('reportContent');

    if (reportDate) {
        reportDate.value = new Date().toISOString().split('T')[0];

        btnLoad.addEventListener('click', loadReport);
        reportDate.addEventListener('change', loadReport);

        loadReport();
    }

    async function loadReport() {
        const date = reportDate.value;
        if (!date) return;

        reportContent.innerHTML = `
            <div class="text-center py-5">
                <div class="spinner-border text-primary" role="status"></div>
                <p class="text-muted mt-2">Cargando reporte...</p>
            </div>
        `;

        try {
            const resp = await fetch('/api/report/' + date);
            const data = await resp.json();

            if (!data.cameras || data.cameras.length === 0) {
                reportContent.innerHTML = `
                    <div class="text-center text-muted py-5">
                        <i class="bi bi-file-earmark-x" style="font-size: 4rem;"></i>
                        <p class="mt-3">Sin capturas para esta fecha</p>
                    </div>
                `;
                return;
            }

            let html = '<div class="row g-4">';
            for (const cam of data.cameras) {
                html += `
                    <div class="col-12">
                        <div class="card bg-dark">
                            <div class="card-header d-flex justify-content-between align-items-center">
                                <span class="fw-bold"><i class="bi bi-camera-video me-2"></i>${cam.camera_name}</span>
                                <span class="badge bg-info">${cam.total_snapshots} capturas</span>
                            </div>
                            <div class="card-body">
                                <div class="d-flex flex-wrap gap-2">`;
                for (const snap of cam.snapshots) {
                    const time = new Date(snap.captured_at).toLocaleTimeString('es-MX', { hour: '2-digit', minute: '2-digit' });
                    html += `
                        <div class="text-center">
                            <a href="#" class="report-snapshot-link" data-snapshot-id="${snap.id}">
                                <img src="/api/snapshots/image/${snap.id}" class="snapshot-thumb-sm" alt="${time}" loading="lazy">
                            </a>
                            <small class="d-block text-muted">${time}</small>
                        </div>`;
                }
                html += `       </div>
                            </div>
                        </div>
                    </div>`;
            }
            html += '</div>';
            reportContent.innerHTML = html;

            document.querySelectorAll('.report-snapshot-link').forEach(el => {
                el.addEventListener('click', function (e) {
                    e.preventDefault();
                    const id = this.dataset.snapshotId;
                    document.getElementById('reportSnapshotImage').src = '/api/snapshots/image/' + id;
                    const modal = new bootstrap.Modal(document.getElementById('reportSnapshotModal'));
                    modal.show();
                });
            });

        } catch (err) {
            reportContent.innerHTML = `
                <div class="alert alert-danger">Error al cargar reporte: ${err.message}</div>
            `;
        }
    }

    // ============ Camera CRUD ============
    const cameraForm = document.getElementById('cameraForm');
    const formTitle = document.getElementById('cameraFormTitle');
    const formCameraId = document.getElementById('formCameraId');
    const formTestResult = document.getElementById('formTestResult');
    const btnTest = document.getElementById('btnTestConnection');

    async function loadForm(cameraId) {
        try {
            const resp = await fetch('/api/cameras/' + cameraId);
            const cam = await resp.json();
            formCameraId.value = cam.id;
            document.getElementById('formName').value = cam.name;
            document.getElementById('formHost').value = cam.host;
            document.getElementById('formPort').value = cam.port;
            document.getElementById('formInterval').value = cam.interval_minutes;
            document.getElementById('formEnabled').checked = cam.enabled;
            document.getElementById('formUsername').value = cam.username;
            document.getElementById('formPassword').value = '';
            formTitle.textContent = 'Editar Cámara';
        } catch (err) {
            showAlert(document.getElementById('cameras-alerts'), 'Error al cargar cámara', 'danger');
        }
    }

    function resetForm() {
        cameraForm.reset();
        formCameraId.value = '';
        formTitle.textContent = 'Nueva Cámara';
        formTestResult.classList.add('d-none');
        document.getElementById('formEnabled').checked = true;
        document.getElementById('formPort').value = '80';
        document.getElementById('formInterval').value = '15';
    }

    document.getElementById('btnNewCamera').addEventListener('click', resetForm);

    document.querySelectorAll('.btn-edit-camera').forEach(btn => {
        btn.addEventListener('click', function () {
            loadForm(this.dataset.cameraId);
        });
    });

    btnTest.addEventListener('click', async function () {
        const data = {
            name: document.getElementById('formName').value || 'test',
            host: document.getElementById('formHost').value,
            port: parseInt(document.getElementById('formPort').value) || 80,
            username: document.getElementById('formUsername').value,
            password: document.getElementById('formPassword').value,
        };
        if (!data.host) {
            showFormTest('Ingresa un host/IP primero', 'warning');
            return;
        }
        btnTest.disabled = true;
        btnTest.innerHTML = '<span class="spinner-border spinner-border-sm"></span> Probando...';
        formTestResult.classList.add('d-none');

        try {
            const resp = await fetch('/api/cameras/test', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(data),
            });
            const result = await resp.json();
            if (result.reachable) {
                showFormTest('Conexión exitosa. Perfiles: ' + (result.profiles.join(', ') || 'ninguno'), 'success');
            } else {
                showFormTest('Error: ' + (result.error || 'no accesible'), 'danger');
            }
        } catch (err) {
            showFormTest('Error de conexión: ' + err.message, 'danger');
        } finally {
            btnTest.disabled = false;
            btnTest.innerHTML = '<i class="bi bi-plug"></i> Probar conexión';
        }
    });

    function showFormTest(msg, type) {
        formTestResult.textContent = msg;
        formTestResult.className = 'alert alert-' + type;
        formTestResult.classList.remove('d-none');
    }

    cameraForm.addEventListener('submit', async function (e) {
        e.preventDefault();
        const isEdit = !!formCameraId.value;
        const data = {
            name: document.getElementById('formName').value,
            host: document.getElementById('formHost').value,
            port: parseInt(document.getElementById('formPort').value) || 80,
            username: document.getElementById('formUsername').value,
            password: document.getElementById('formPassword').value,
            interval_minutes: parseInt(document.getElementById('formInterval').value) || 15,
            enabled: document.getElementById('formEnabled').checked,
        };

        try {
            let resp;
            if (isEdit) {
                resp = await fetch('/api/cameras/' + formCameraId.value, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(data),
                });
            } else {
                resp = await fetch('/api/cameras', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(data),
                });
            }
            if (!resp.ok) throw new Error('Error al guardar');
            const modal = bootstrap.Modal.getInstance(document.getElementById('cameraFormModal'));
            modal.hide();
            location.reload();
        } catch (err) {
            showFormTest('Error: ' + err.message, 'danger');
        }
    });

    document.querySelectorAll('.btn-delete-camera').forEach(btn => {
        btn.addEventListener('click', async function () {
            const id = this.dataset.cameraId;
            const name = this.dataset.name;
            if (!confirm('¿Eliminar cámara "' + name + '"?')) return;
            try {
                const resp = await fetch('/api/cameras/' + id, { method: 'DELETE' });
                if (resp.ok || resp.status === 204) {
                    location.reload();
                } else {
                    showAlert(document.getElementById('cameras-alerts'), 'Error al eliminar', 'danger');
                }
            } catch (err) {
                showAlert(document.getElementById('cameras-alerts'), 'Error: ' + err.message, 'danger');
            }
        });
    });

    document.querySelectorAll('.btn-test-camera').forEach(btn => {
        btn.addEventListener('click', async function () {
            const id = this.dataset.cameraId;
            const alerts = document.getElementById('cameras-alerts');
            try {
                const resp = await fetch('/api/cameras/' + id + '/snapshot', { method: 'POST' });
                const data = await resp.json();
                if (data.success) {
                    showAlert(alerts, 'Captura de prueba exitosa', 'success');
                } else {
                    showAlert(alerts, 'Error: ' + (data.error || 'desconocido'), 'danger');
                }
            } catch (err) {
                showAlert(alerts, 'Error: ' + err.message, 'danger');
            }
        });
    });

    // ============ Helpers ============
    function showAlert(container, message, type) {
        const alert = document.createElement('div');
        alert.className = 'alert alert-' + type + ' alert-dismissible fade show';
        alert.innerHTML = message + '<button type="button" class="btn-close" data-bs-dismiss="alert"></button>';
        container.appendChild(alert);
        setTimeout(() => alert.remove(), 5000);
    }
});
