Component({
  options: { addGlobalClass: true },
  properties: {
    content: { type: String, value: '' },
    done: { type: Boolean, value: false },
    frozen: { type: Boolean, value: false },
  },
  methods: {
    onTap() {
      this.triggerEvent('toggle')
    },
  },
})
