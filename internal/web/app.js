'use strict'

const app = document.querySelector('#app')
const configuration = JSON.parse(app.dataset.configuration)

Scalar.createApiReference('#app', configuration)
