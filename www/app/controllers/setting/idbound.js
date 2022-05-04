import Ember from 'ember';

export default Ember.Controller.extend({
  options: ['allow', 'deny'],
  options2: ['none', 'slack'],
  selectedRule: null,
  selectedAlarm: null,
  selectedDesc: null,
  textID: "",
  textDesc: "",

  init() {
    this._super(...arguments);
    this.errors = [];
    this.textID = "";
    this.textDesc = "";
    this.selectedRule = this.options[0];
    this.selectedAlarm = this.options2[0];
    this.selectedDesc = ""
  },

  didUpdateAttrs() {
    this._super(...arguments);
    this.set('errors', []);
  },

  actions: {
    required(event) {
      if (!event.target.value) {
        this.get('errors').pushObject({ message: `${event.target.name} is required`});
      } else {
        this.textID = event.target.value;
      }
    }
  }
});
