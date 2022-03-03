import Ember from 'ember';

export default Ember.Controller.extend({
  devid: "",
  subid: "",
  amount: "0",
  allowid: true,
  init() {
    this._super(...arguments);
    this.errors = [];
    this.devid = "";
    this.subid = "";
    this.amount = "0";
    this.allowid = true;
  },

  cachedDevId: Ember.computed('devid', {
    get() {
      return localStorage.getItem('devid');
    },
    // set(key, value) {
    //   //localStorage.setItem('devid',value)
    //   return value;
    // }
  }),
  actions: {
    required(event) {
      if (!event.target.value) {
        this.get('errors').pushObject({ message: `${event.target.name} is required`});
      } else {
        var result = event.target.value.match(/\b[0-9A-Fa-f]{1,40}\b/g);
        console.log(result)
        if (result) {
          this.devid = event.target.value;
        } else {
          this.get('errors').pushObject({ message: `${event.target.name} is not values`});
        }

      }
    },
    required2(event) {
      if (!event.target.value) {
        this.get('errors').pushObject({ message: `${event.target.name} is required`});
      } else {
        this.subid = event.target.value;
      }
    }
  }

});
