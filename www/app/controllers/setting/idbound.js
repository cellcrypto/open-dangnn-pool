import Ember from 'ember';

export default Ember.Controller.extend({
  options: ['allow', 'deny'],
  selectedRule: null,
  textID: "",

  init() {
    this._super(...arguments);
    this.errors = [];
    this.textID = "";
    this.selectedRule = this.options[0];
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
